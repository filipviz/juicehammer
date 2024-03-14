// names provides simple heuristic-based checks for new users and some messages to prevent spam and impersonation.
package names

import (
	"fmt"
	"juicehammer/juicebox"
	"log"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
)

var contributors []string // Slice of contributor usernames/global names/nicknames to check for (to prevent impersonation)

// Build a slice of contributor usernames and nicknames to check new users against
func BuildContributorsList(s *discordgo.Session) {
	if contributors != nil {
		log.Println("buildContributorsList called more than once")
		return
	}

	contributors = make([]string, 0)

	// Add nicknames and usernames for users with contributor or admin roles to the map
	var after string
	for {
		mems, err := s.GuildMembers(juicebox.JuiceboxGuildId, after, 1000)
		if err != nil {
			log.Fatalf("Error getting guild members: %s\n", err)
		}

	memLoop:
		for _, mem := range mems {
			for _, r := range mem.Roles {
				if r == juicebox.ContributorRoleId || r == juicebox.AdminRoleId {
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
type recentSpam struct {
	count      int
	channelIds []string
	msgs       []string
}

// Map of user IDs to the number of messages they've sent in the last 100 seconds
var spamTracker = struct {
	sync.RWMutex // Fine to have one lock for the whole struct since reads/writes are infrequent. At scale, would need to optimize.
	recent       map[string]recentSpam
}{
	recent: make(map[string]recentSpam),
}

// When a recently joined user sends a message, check if they've sent messages to many channels recently, and mute them if they have.
func CheckSpam(s *discordgo.Session, m *discordgo.MessageCreate) {
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
		if r == juicebox.ContributorRoleId || r == juicebox.AdminRoleId || r == juicebox.AlumniRoleId {
			return
		}
	}

	spamTracker.RLock()
	r, ok := spamTracker.recent[m.Author.ID]
	spamTracker.RUnlock()

	// If not found, initialize the user's spam tracker
	if !ok {
		spamTracker.Lock()
		spamTracker.recent[m.Author.ID] = recentSpam{
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
		// If the user has sent more than 2 messages in the past 2 minutes, investigate further
		if len(r.msgs) > 2 {
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
				juicebox.MuteMember(s, m.Author.ID, muteMsg, muteTime)
			}
		}

		spamTracker.Lock()
		spamTracker.recent[m.Author.ID] = recentSpam{
			count:      r.count + 1,
			channelIds: append(r.channelIds, m.ChannelID),
			msgs:       append(r.msgs, m.Content),
		}
		spamTracker.Unlock()
	}
}

// When a user joins, check if their username, nickname, or global name is suspicious, and mute them if it is.
func UserJoins(s *discordgo.Session, m *discordgo.GuildMemberAdd) {
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
			muteMsg := fmt.Sprintf("%s joined with a suspicious %s ('%s', matches '%s'). Muting until <t:%d>.", m.User.Mention(), k, v, match, muteTime.Unix())
			juicebox.MuteMember(s, m.User.ID, muteMsg, muteTime)
			return
		}
	}
}

// When a user is updated, check if their username, global name, or nickname is suspicious and mute them if it is.
func MemberUpdate(s *discordgo.Session, m *discordgo.GuildMemberUpdate) {
	// If the member is already muted, skip.
	if m.Mute {
		return
	}

	// If the member did not change any of their names, skip.
	if m.BeforeUpdate != nil &&
		m.BeforeUpdate.Nick == m.Nick &&
		m.BeforeUpdate.User.GlobalName == m.User.GlobalName &&
		m.BeforeUpdate.User.Username == m.User.Username {
		return
	}

	// If the user has a contributor, admin, or alumni role, don't check their nickname
	for _, r := range m.Roles {
		if r == juicebox.ContributorRoleId || r == juicebox.AdminRoleId || r == juicebox.AlumniRoleId {
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
			muteMsg := fmt.Sprintf("%s switched to a suspicious %s ('%s', matches '%s'). Muting until <t:%d>.", m.User.Mention(), k, v, match, muteTime.Unix())
			juicebox.MuteMember(s, m.User.ID, muteMsg, muteTime)
			return
		}
	}
}

var susWords = []string{"support", "juicebox", "announcement", "airdrop", "admin", "giveaway", "opensea", "uniswap", "reward", "ticket", "metamask"} // A list of suspicious words to check for

var containsWords = []string{"📢", "📣", "📡"} // A list of emojis/words to check for (only if the names contain - they are too short for meaningful levenshtein distance calculations)

// Checks whether the given string is suspicious and what it matches (both suspicious words and contributor names)
func isSus(toCheck string) (is bool, match string) {
	// Normalize to lower case
	norm := strings.ToLower(toCheck)

	// Check if the string contains a suspicious emoji/word
	for _, w := range containsWords {
		if strings.Contains(norm, w) {
			return true, w
		}
	}

	// Check against suspicious words with a levenshtein distance of 2
	for _, w := range susWords {
		if strings.Contains(norm, w) || (len(norm) > 4 && levenshtein(norm, w) <= len(norm)/4) {
			return true, w
		}
	}

	// Check against contributor names with a levenshtein distance of 2
	for _, w := range contributors {
		if len(norm) > 4 && levenshtein(norm, w) <= len(norm)/4 {
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
