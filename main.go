package main

import (
	"juicehammer/names"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
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

	names.BuildContributorsList(s)

	s.AddHandler(names.UserJoins)
	s.AddHandler(names.MemberUpdate)
	s.AddHandler(names.CheckSpam)
	log.Println("Now monitoring the server.")

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	log.Println("Shutting down...")
}
