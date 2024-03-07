package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
)

const (
	juiceboxGuildId     = "775859454780244028"
	contributorRoleId   = "865459358434590740"
	adminRoleId         = "864238986456989697"
	alumniRoleId        = "1091786430046552097"
	operationsChannelId = "889560116666441748"
)

var s *discordgo.Session

func init() {
	_, err := os.Stat(".env")
	if !os.IsNotExist(err) {
		err := godotenv.Load()
		if err != nil {
			log.Fatalf("Could not load .env file: %s\n", err)
		}
	}

	d := os.Getenv("DISCORD_TOKEN")
	if d == "" {
		log.Fatal("Could not find DISCORD_TOKEN environment variable")
	}

	s, err = discordgo.New("Bot " + d)
	if err != nil {
		log.Fatalf("Error creating Discord session: %s\n", err)
	}

	s.Identify.Intents = discordgo.IntentGuildMembers
}

func main() {
	err := s.Open()
	if err != nil {
		log.Fatalf("Error opening connection to Discord: %s\n", err)
	}
	defer s.Close()

	buildContributorsList()
	s.AddHandler(userJoins)
	s.AddHandler(memberUpdate)
	s.AddHandler(checkSpam)
	log.Println("Now monitoring the server.")

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	log.Println("Shutting down...")
}

var contributors []string // Slice of contributor usernames/global names/nicknames to check for (to prevent impersonation)

// Build a slice of contributor usernames and nicknames to check new users against
func buildContributorsList() {
	if contributors != nil {
		log.Println("buildContributorsList called more than once")
		return
	}

	contributors = make([]string, 0)

	// Add nicknames and usernames for users with contributor, admin, or alumni roles to the map
	var after string
	for {
		mems, err := s.GuildMembers(juiceboxGuildId, after, 1000)
		if err != nil {
			log.Fatalf("Error getting guild members: %s\n", err)
		}

	memLoop:
		for _, mem := range mems {
			for _, r := range mem.Roles {
				if r == contributorRoleId || r == adminRoleId || r == alumniRoleId {
					username := strings.ToLower(mem.User.Username)
					nick := strings.ToLower(mem.Nick)
					global := strings.ToLower(mem.User.GlobalName)

					if username != "" {
						contributors = append(contributors, username)
					}
					if nick != "" {
						contributors = append(contributors, nick)
					}
					if global != "" {
						contributors = append(contributors, global)
					}

					continue memLoop
				}
			}
		}

		// If we get less than 1000 members, we're done
		if len(mems) < 1000 {
			break
		}

		// Update the after ID for the next iteration
		after = mems[len(mems)-1].User.ID
	}

	// Sort and compact the contributors list to remove duplicates
	slices.Sort(contributors)
	contributors = slices.Compact(contributors)

	log.Printf("Built contributors list with %d entries: %v\n", len(contributors), contributors)
}

// Holds recent message information for a user
type RecentSpam struct {
	count      int
	channelIds []string
	msgs       []string
}

// Map of user IDs to the number of messages they've sent in the last 100 seconds
var spamTracker = struct {
	sync.RWMutex // Fine to have one lock for the whole struct since reads/writes are infrequent. At scale, would need to optimize.
	recent       map[string]RecentSpam
}{
	recent: make(map[string]RecentSpam),
}

// When a recently joined user sends a message, check if they've sent messages to many channels recently, and mute them if they have.
func checkSpam(s *discordgo.Session, m *discordgo.MessageCreate) {
	// If the user joined more than a week ago, don't check their messages
	if time.Since(m.Member.JoinedAt) > time.Hour*24*7 {
		return
	}

	// If the author is a bot or nil, return
	if m.Author.Bot || m.Author == nil {
		return
	}

	// If the user has a contributor, admin, or alumni role, don't check their messages
	for _, r := range m.Member.Roles {
		if r == contributorRoleId || r == adminRoleId || r == alumniRoleId {
			return
		}
	}

	spamTracker.RLock()
	r, ok := spamTracker.recent[m.Author.ID]
	spamTracker.RUnlock()

	// If not found, initialize the user's spam tracker
	if !ok {
		spamTracker.Lock()
		spamTracker.recent[m.Author.ID] = RecentSpam{
			count:      1,
			channelIds: []string{m.ChannelID},
			msgs:       []string{m.Content},
		}
		spamTracker.Unlock()

		// After 2 minutes, clear the spam tracker for this user
		go func() {
			time.Sleep(2 * time.Minute)
			spamTracker.Lock()
			delete(spamTracker.recent, m.Author.ID)
			spamTracker.Unlock()
		}()
	} else {
		// If the user has sent more than 3 messages in the past 2 minutes, investigate further
		if len(r.msgs) > 3 {
			slices.Sort(r.msgs)
			compactMsgs := slices.Compact(r.msgs)
			// If the compact slice is shorter than the original, the user has sent the same message multiple times
			if len(compactMsgs) < len(r.msgs) {
				// So we mute them
				muteTime := time.Now().Add(1 * time.Hour)
				muteMsg := fmt.Sprintf("Muting %s until <t:%d> for sending %d messages in the last 2 minutes in channels:", m.Author.Mention(), muteTime.Unix(), r.count)
				for _, c := range slices.Compact(r.channelIds) {
					muteMsg += fmt.Sprintf(" <#%s>", c)
				}
				muteMsg += ". Most recent content: \n> " + r.msgs[len(r.msgs)-1]
				muteMember(m.Author.ID, muteMsg, muteTime)
			}
		}

		spamTracker.Lock()
		spamTracker.recent[m.Author.ID] = RecentSpam{
			count:      r.count + 1,
			channelIds: append(r.channelIds, m.ChannelID),
			msgs:       append(r.msgs, m.Content),
		}
		spamTracker.Unlock()
	}
}

// When a user joins, check if their username, nickname, or global name is suspicious, and mute them if it is.
func userJoins(s *discordgo.Session, m *discordgo.GuildMemberAdd) {
	toCheck := map[string]string{
		"username":    m.User.Username,
		"global name": m.User.GlobalName,
		"nickname":    m.Nick,
	}

	for k, v := range toCheck {
		if v == "" {
			continue
		}

		if is, match := isSus(v); is {
			muteTime := time.Now().Add(24 * time.Hour)
			muteMsg := fmt.Sprintf("%s joined with a suspicious %s ('%s', close to '%s'). Muting until <t:%d>.", m.User.Mention(), k, v, match, muteTime.Unix())
			muteMember(m.User.ID, muteMsg, muteTime)
			return
		}
	}
}

// When a user is updated, check if their username, global name, or nickname is suspicious and mute them if it is.
func memberUpdate(s *discordgo.Session, m *discordgo.GuildMemberUpdate) {
	// If the member is already muted, skip.
	if m.Mute {
		return
	}

	// If the member did not change any of their names, skip.
	if m.BeforeUpdate.Nick == m.Nick &&
		m.BeforeUpdate.User.GlobalName == m.User.GlobalName &&
		m.BeforeUpdate.User.Username == m.User.Username {
		return
	}

	// If the user has a contributor, admin, or alumni role, don't check their nickname
	for _, r := range m.Roles {
		if r == contributorRoleId || r == adminRoleId || r == alumniRoleId {
			return
		}
	}

	toCheck := map[string]string{
		"username":    m.User.Username,
		"global name": m.User.GlobalName,
		"nickname":    m.Nick,
	}

	for k, v := range toCheck {
		if v == "" {
			continue
		}

		if is, match := isSus(v); is {
			muteTime := time.Now().Add(24 * time.Hour)
			muteMsg := fmt.Sprintf("%s switched to a suspicious %s ('%s', close to '%s'). Muting until <t:%d>.", m.User.Mention(), k, v, match, muteTime.Unix())
			muteMember(m.User.ID, muteMsg, muteTime)
			return
		}
	}
}

// Mute a member and send a message to the operations channel.
func muteMember(userId string, muteMsg string, until time.Time) {
	if _, err := s.ChannelMessageSend(operationsChannelId, muteMsg); err != nil {
		log.Printf("Error sending message '%s' to operations channel: %s\n", muteMsg, err)
	}

	if err := s.GuildMemberTimeout(juiceboxGuildId, userId, &until); err != nil {
		log.Printf("Error muting user '%s' with message '%s' until %s: %s\n", userId, muteMsg, until, err)
		return
	}

	log.Printf("Muted user %s with message %s until %s\n", userId, muteMsg, until)
}

var susWords = []string{"support", "juicebox", "announcement", "airdrop", "admin", "giveaway"} // A list of suspicious words to check for
var containsWords = []string{"📢", "📣"}                                                         // A list of emojis to check for (only if the names contain - they are too short for meaningful levenshtein distance calculations)

// Checks whether the given string is suspicious and what it matches (both suspicious words and contributor names)
func isSus(s string) (is bool, match string) {
	s = strings.ToLower(s)

	// Check if the string contains a suspicious emoji/word
	for _, w := range containsWords {
		if strings.Contains(s, w) {
			return true, w
		}
	}

	// Check against suspicious words with a levenshtein distance of 2
	for _, w := range susWords {
		if strings.Contains(s, w) || levenshtein(s, w) <= 2 {
			return true, w
		}
	}

	// Check against contributor names with a levenshtein distance of 1
	for _, w := range contributors {
		if strings.Contains(s, w) || levenshtein(s, w) <= 1 {
			return true, w
		}
	}

	return false, ""
}

// minLengthThreshold is the length of the string beyond which an allocation will be made. Strings smaller than this will be zero alloc.
const minLengthThreshold = 32

// Returns true if the levenshtein distance between a and b is less than or equal to distance
// This is a reduced implementation based on https://github.com/agnivade/levenshtein/blob/master/levenshtein.go
func levenshtein(a, b string) int {
	if len(a) == 0 {
		return utf8.RuneCountInString(b)
	}

	if len(b) == 0 {
		return utf8.RuneCountInString(a)
	}

	if a == b {
		return 0
	}

	// Normalize the strings to lowercase runes
	s1 := []rune(strings.ToLower(a))
	s2 := []rune(strings.ToLower(b))

	// swap to save some memory O(min(a,b)) instead of O(a)
	if len(s1) > len(s2) {
		s1, s2 = s2, s1
	}
	lenS1 := len(s1)
	lenS2 := len(s2)

	// Create a slice of ints to hold the previous and current cost
	var x []uint16
	if lenS1+1 > minLengthThreshold {
		x = make([]uint16, lenS1+1)
	} else {
		// Optimization for small strings.
		// We can reslice to save memory.
		x = make([]uint16, minLengthThreshold)
		x = x[:lenS1+1]
	}

	for i := 1; i < len(x); i++ {
		x[i] = uint16(i)
	}

	// make a dummy bounds check to prevent the 2 bounds check down below.
	// The one inside the loop is costly.
	_ = x[lenS1]
	for i := 1; i <= lenS2; i++ {
		prev := uint16(i)
		for j := 1; j <= lenS1; j++ {
			current := x[j-1] // match
			if s2[i-1] != s1[j-1] {
				current = min(x[j-1]+1, prev+1, x[j]+1)
			}
			x[j-1] = prev
			prev = current
		}
		x[lenS1] = prev
	}

	return int(x[lenS1])
}
