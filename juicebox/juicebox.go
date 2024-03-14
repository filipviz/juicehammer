// Constants and functions specific to the JuiceboxDAO Discord server.
package juicebox

import (
	"log"
	"time"

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
