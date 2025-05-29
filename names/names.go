// names provides simple heuristic-based checks for new users and some messages to prevent spam and impersonation.
package names

import (
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"
)

// Slice of contributor usernames/global names/nicknames to check for (to prevent impersonation)
var monitored = struct {
	sync.RWMutex
	names []string
}{names: make([]string, 0)}

// Parse the names of a member and add them to the list of names to check against.
func MonitorName(mem *discordgo.Member) {
	monitored.Lock()
	defer monitored.Unlock()

	if username := normalizeString(mem.User.Username); username != "" {
		monitored.names = append(monitored.names, username)
	}
	if nick := normalizeString(mem.Nick); nick != "" {
		monitored.names = append(monitored.names, nick)
	}
	if global := normalizeString(mem.User.GlobalName); global != "" {
		monitored.names = append(monitored.names, global)
	}
}

// A list of known suspicious words to check for
var susWords = []string{"support", "juicebox", "announcement", "airdrop", "admin", "giveaway", "opensea", "uniswap", "reward", "ticket", "metamask", "help", "kathryn"}

// A list of known emojis/words to check for (only if the names contain them - they are too short for meaningful levenshtein distance calculations)
var containsWords = []string{"📢", "📣", "📡", "🎁"}

// Normalizes a string by converting it to lowercase and applying NFKC normalization.
func normalizeString(s string) string {
	return norm.NFKC.String(cases.Lower(language.Und, cases.NoLower).String(s))
}

// Checks whether the given string is suspicious and what it matches (both suspicious words and contributor names)
func NameIsSuspicious(name string) (is bool, match string) {
	// Normalize the input name
	norm := normalizeString(name)

	// Check if the string contains a suspicious emoji/word
	for _, w := range containsWords {
		if strings.Contains(norm, w) {
			return true, w
		}
	}

	// Check against suspicious words with a levenshtein distance
	for _, w := range susWords {
		if strings.Contains(norm, w) || (len(norm) > 4 && levenshtein(norm, w) <= len(norm)/4) {
			return true, w
		}
	}

	monitored.RLock()
	defer monitored.RUnlock()
	// Check against contributor names with a levenshtein distance
	for _, w := range monitored.names {
		// monitored names are already normalized
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

	// Strings are already normalized and lowercased by the caller
	s1 := []rune(a)
	s2 := []rune(b)

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
