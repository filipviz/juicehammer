// Prevents users from spamming messages in multiple channels in a short period of time
package spam

import (
	"bytes"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Holds recent message information for a user.
type recentMsgs struct {
	channelIds []string
	msgs       []string
}

// Map of user IDs to the number of messages they've sent in the last 100 seconds
var spamTracker = struct {
	sync.RWMutex // Fine to have one lock for the whole struct since reads/writes are infrequent. At scale, would need to optimize.
	recent       map[string]recentMsgs
}{recent: make(map[string]recentMsgs)}

// Check if a message's creator has sent messages to many channels recently.
// Returns whether the user is spamming and a list of the channels they've sent messages to (as a string).
func IsSpam(m *discordgo.MessageCreate) (is bool, channels string) {
	spamTracker.RLock()
	r, ok := spamTracker.recent[m.Author.ID]
	spamTracker.RUnlock()

	// If not found, initialize the user's spam tracker
	if !ok {
		spamTracker.Lock()
		spamTracker.recent[m.Author.ID] = recentMsgs{
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
		// If found, and there are more than 2 messages, investigate further.
		if len(r.msgs) > 2 {
			slices.Sort(r.channelIds)
			channelIds := slices.Compact(r.channelIds)

			// If the user has sent messages to more than 2 channels in the last 2 minutes, mute them.
			if len(channelIds) > 2 {

				var channels bytes.Buffer
				for _, c := range slices.Compact(r.channelIds) {
					channels.WriteString(fmt.Sprintf(" <#%s>", c))
				}
				return true, channels.String()
			}
		}

		spamTracker.Lock()
		spamTracker.recent[m.Author.ID] = recentMsgs{
			channelIds: append(r.channelIds, m.ChannelID),
			msgs:       append(r.msgs, m.Content),
		}
		spamTracker.Unlock()
	}

	return false, ""
}
