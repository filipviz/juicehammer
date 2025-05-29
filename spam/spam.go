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

const coolDownPeriod = 2 * time.Minute

// Holds recent message information and activity timer for a user.
type userSpamInfo struct {
	channelIds []string
	msgs       []string
	timer      *time.Timer // Timer to clear this user's entry after coolDownPeriod of inactivity
}

// Map of user IDs to their spam tracking information.
var spamTracker = struct {
	sync.RWMutex // Using RWMutex, IsSpam will use Lock for modifications.
	users map[string]*userSpamInfo
}{users: make(map[string]*userSpamInfo)}

// IsSpam checks if a message constitutes spam based on recent activity.
// It flags a user as spamming if they send more than 3 messages to more than 3 unique channels
// within the coolDownPeriod. Each message resets the cooldown timer.
func IsSpam(m *discordgo.MessageCreate) (is bool, channelsOutput string) {
	spamTracker.Lock() // Acquire a full lock as we are likely modifying state
	defer spamTracker.Unlock() // Ensure unlock happens on all return paths

	userInfo, ok := spamTracker.users[m.Author.ID]
	if !ok {
		// New user
		userInfo = &userSpamInfo{
			channelIds: []string{m.ChannelID},
			msgs:       []string{m.Content},
		}
		// Assign the timer directly
		userInfo.timer = time.AfterFunc(coolDownPeriod, func() {
			// Callback for the end of the coolDownPeriod initiated by this timer.
			spamTracker.Lock()
			delete(spamTracker.users, m.Author.ID)
			spamTracker.Unlock()
		})
		spamTracker.users[m.Author.ID] = userInfo
		return false, ""
	}

	// Existing user - reset their inactivity timer
	if userInfo.timer != nil {
		userInfo.timer.Stop() // Stop the previous timer
	}
	userInfo.timer = time.AfterFunc(coolDownPeriod, func() {
		// Re-initialize the callback.
		spamTracker.Lock()
		delete(spamTracker.users, m.Author.ID)
		spamTracker.Unlock()
	})

	// Append current message's details
	userInfo.channelIds = append(userInfo.channelIds, m.ChannelID)
	userInfo.msgs = append(userInfo.msgs, m.Content)

	// Check for spam - our heuristic is that sending 3+ messages to 3+ unique channels is probably spam.
	if len(userInfo.msgs) > 3 {
		// Create a copy to avoid modifying the original userInfo.channelIds
		tempChannelIds := make([]string, len(userInfo.channelIds))
		copy(tempChannelIds, userInfo.channelIds)
		slices.Sort(tempChannelIds)
		uniqueChannelIds := slices.Compact(tempChannelIds)

		if len(uniqueChannelIds) > 3 {
			var channelsBuffer bytes.Buffer
			for _, c := range uniqueChannelIds {
				channelsBuffer.WriteString(fmt.Sprintf(" <#%s>", c))
			}
			return true, channelsBuffer.String()
		}
	}

	return false, ""
}
