// Constants and functions specific to the JuiceboxDAO Discord server.
package juicebox

import (
	"fmt"
	"log"
	"time"

	"github.com/filipviz/juicehammer/names"
	"github.com/filipviz/juicehammer/pfp"
	"github.com/filipviz/juicehammer/spam"

	"github.com/bwmarrin/discordgo"
)

const (
	JuiceboxGuildId     = "775859454780244028"
	ContributorRoleId   = "865459358434590740"
	AdminRoleId         = "864238986456989697"
	AlumniRoleId        = "1091786430046552097"
	OperationsChannelId = "889560116666441748"
)

// Mute a member and send a message to the operations channel.
func MuteMember(s *discordgo.Session, userId string, muteMsg string, until time.Time) {
	if _, err := s.ChannelMessageSend(OperationsChannelId, muteMsg); err != nil {
		log.Printf("Error sending message '%s' to operations channel: %s\n", muteMsg, err)
	}

	if err := s.GuildMemberTimeout(JuiceboxGuildId, userId, &until); err != nil {
		log.Printf("Error muting user '%s' with message '%s' until %s: %s\n", userId, muteMsg, until, err)
		return
	}

	log.Printf("Muted user %s with message %s until %s\n", userId, muteMsg, until)
}

// Iterate through JuiceboxDAO Discord server members and add contributors' names and PFPs to lists to check against.
func ParseContributors(s *discordgo.Session) {
	var after string
	for {
		mems, err := s.GuildMembers(JuiceboxGuildId, after, 1000)
		if err != nil {
			log.Fatalf("Error getting guild members: %s\n", err)
		}

	memLoop:
		for _, mem := range mems {
			for _, r := range mem.Roles {
				if r == ContributorRoleId || r == AdminRoleId {
					names.MonitorName(mem)

					go pfp.MonitorPfp(
						mem.AvatarURL("256"),
						fmt.Sprintf("%s's profile picture", mem.Mention()),
					)

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

	// Sort and compact the lists to remove duplicates

	log.Printf("PFPs and names parsed for contributors and admins.\n")
}

// When a user joins, check if their names or PFP is suspicious, and mute them if either one is.
func ScreenOnJoin(s *discordgo.Session, m *discordgo.GuildMemberAdd) {
	// If the member is already muted, skip.
	if m.Mute {
		return
	}

	checkNames(s, m.Member)
	checkPfp(s, m.Member)
}

// When a user is updated, check if their names or PFP are suspicious and mute them if either one is.
func ScreenOnUpdate(s *discordgo.Session, m *discordgo.GuildMemberUpdate) {
	// If the member is already muted, skip.
	if m.Mute {
		return
	}

	// If the user has a contributor or admin role, skip.
	for _, r := range m.Roles {
		if r == ContributorRoleId || r == AdminRoleId {
			return
		}
	}

	// If we can't get information before the update, check both to be safe.
	if m.BeforeUpdate == nil {
		checkNames(s, m.Member)
		checkPfp(s, m.Member)
		return
	}

	// If the member changed any of their names, check them.
	if m.BeforeUpdate.Nick != m.Nick ||
		m.BeforeUpdate.User.GlobalName != m.User.GlobalName ||
		m.BeforeUpdate.User.Username != m.User.Username {
		checkNames(s, m.Member)
	}

	// If the user changed their PFP, check it.
	if m.BeforeUpdate.AvatarURL("256") != m.AvatarURL("256") {
		checkPfp(s, m.Member)
	}
}

// When a message is sent, check if it is spam and mute the author if it is.
func ScreenMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	// If the author is a bot, nil, or already muted, skip the check.
	if m.Author.Bot || m.Author == nil || m.Member.Mute {
		return
	}

	// If the user has a contributor or admin role, skip the check.
	for _, r := range m.Member.Roles {
		if r == ContributorRoleId || r == AdminRoleId {
			return
		}
	}

	checkSpam(s, m)
}

// Check a user's names and mute them if any of them are suspicious.
func checkNames(s *discordgo.Session, m *discordgo.Member) {
	toCheck := map[string]string{
		"username":    m.User.Username,
		"global name": m.User.GlobalName,
		"nickname":    m.Nick,
	}

	for k, v := range toCheck {
		if v == "" {
			continue
		}

		if is, match := names.NameIsSuspicious(v); is {
			muteTime := time.Now().Add(24 * time.Hour)
			muteMsg := fmt.Sprintf("%s has a suspicious %s ('%s', matches '%s'). Muting until <t:%d>.", m.User.Mention(), k, v, match, muteTime.Unix())
			MuteMember(s, m.User.ID, muteMsg, muteTime)
			return
		}
	}
}

// Check a user's PFP and mute them if it is suspicious.
func checkPfp(s *discordgo.Session, m *discordgo.Member) {
	is, msg, err := pfp.PfpIsSuspicious(m)
	if err != nil {
		log.Printf("Error checking PFP for user %s: %v\n", m.User.ID, err)
		return
	}

	if is {
		muteTime := time.Now().Add(24 * time.Hour)
		MuteMember(s, m.User.ID, msg, muteTime)
	}
}

// Check if a message is spam and mute the author if it is.
func checkSpam(s *discordgo.Session, m *discordgo.MessageCreate) {
	if is, channels := spam.IsSpam(m); is {
		muteTime := time.Now().Add(24 * time.Hour)
		muteMsg := fmt.Sprintf("Muting %s until <t:%d> for possible spamming. Channels:%s.",
			m.Author.Mention(), muteTime.Unix(), channels)
		MuteMember(s, m.Author.ID, muteMsg, muteTime)
	}
}
