package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
)

const (
	JUICEBOX_GUILD_ID     = "775859454780244028"
	CONTRIBUTOR_ROLE_ID   = "865459358434590740"
	ADMIN_ROLE_ID         = "864238986456989697"
	ALUMNI_ROLE_ID        = "1091786430046552097"
	OPERATIONS_CHANNEL_ID = "889560116666441748"
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
	log.Println("Now monitoring new users.")

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	log.Println("Shutting down...")
}

var contributors []string // Slice of suspicious words to check for in usernames
var susWords = []string{"support", "juicebox", "announcements", "airdrop", "admin", "giveaway"}

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
		mems, err := s.GuildMembers(JUICEBOX_GUILD_ID, after, 1000)
		if err != nil {
			log.Fatalf("Error getting guild members: %s\n", err)
		}

	memLoop:
		for _, mem := range mems {
			for _, r := range mem.Roles {
				if r == CONTRIBUTOR_ROLE_ID || r == ADMIN_ROLE_ID || r == ALUMNI_ROLE_ID {
					username := strings.ToLower(mem.User.Username)
					nick := strings.ToLower(mem.Nick)

					if username != "" {
						contributors = append(contributors, username)
					}
					if nick != "" {
						contributors = append(contributors, nick)
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

	// Sort and compact the contributors list
	slices.Sort(contributors)
	contributors = slices.Compact(contributors)

	log.Printf("Built contributors list with %d entries: %v\n", len(contributors), contributors)
}

// A handler to check whether a user joining is suspicious. If they are, mute them for 24 hours and send a message saying they were muted.
func userJoins(s *discordgo.Session, m *discordgo.GuildMemberAdd) {
	muteTime := time.Now().Add(24 * time.Hour)
	var sus bool
	var msg string

	if is, match := isSus(m.User.Username); is {
		sus = true
		msg = fmt.Sprintf("User <@%s> joined with a suspicious username (close to %s). Muting until <t:%d>.", m.User.Username, match, muteTime.Unix())
		log.Println(msg)
		goto Mute
	}

	if m.Nick != "" {
		if is, match := isSus(m.Nick); is {
			sus = true
			msg = fmt.Sprintf("User <@%s> joined with a suspicious nickname %s (close to %s). Muting until <t:%d>.", m.User.ID, m.Nick, match, muteTime.Unix())
			log.Println(msg)
		}
	}

Mute:
	if sus {
		if _, err := s.ChannelMessageSend(OPERATIONS_CHANNEL_ID, msg); err != nil {
			log.Printf("Error sending message to operations channel: %s\n", err)
		}

		if err := s.GuildMemberTimeout(JUICEBOX_GUILD_ID, m.User.ID, &muteTime); err != nil {
			log.Printf("Error muting user: %s\n", err)
		}
	}
}

// Checks whether the given string is suspicious and what it matches (both suspicious words and contributor names)
func isSus(s string) (is bool, match string) {
	s = strings.ToLower(s)

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
