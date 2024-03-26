package main

import (
	"juicehammer/juicebox"
	"juicehammer/pfp"
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

	// Prepare lists to check against.
	juicebox.ParseContributors(s)
	pfp.HashFolderImgs()

	// Add handlers.
	s.AddHandler(juicebox.ScreenOnJoin)
	s.AddHandler(juicebox.ScreenOnUpdate)
	s.AddHandler(juicebox.ScreenMessage)
	log.Println("Now monitoring the server.")

	// Wait for a signal to exit.
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	log.Println("Shutting down...")
}
