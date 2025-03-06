package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
)

var (
	GuildID        = flag.String("guild", "", "Test guild ID. If not passed - bot registers commands globally") // flags allow to set the type, value and help texts
	BotToken       = flag.String("token", "", "Bot access token")
	RemoveCommands = flag.Bool("rmcmd", true, "Remove all commands after shutdowning or not")
)

var s *discordgo.Session

func init() {
	// Load .env file first
	err := godotenv.Load()
	if err != nil {
		log.Println("Warning: Error loading .env file:", err)
	}

	// Parse flags
	flag.Parse()

	// Override with environment variables if they exist and flags weren't explicitly set
	if flag.Lookup("guild").Value.String() == "" {
		if guildID := os.Getenv("GUILD_ID"); guildID != "" {
			*GuildID = guildID
		}
	}

	if flag.Lookup("token").Value.String() == "" {
		if botToken := os.Getenv("BOT_TOKEN"); botToken != "" {
			*BotToken = botToken
		}
	}

	// Initialize Discord session
	var err2 error
	s, err2 = discordgo.New("Bot " + *BotToken)
	if err2 != nil {
		log.Fatal("Invalid bot parameters: ", err2)
	}
}

var (
	// Each command must have a description, otherwise it won't register
	commands = []*discordgo.ApplicationCommand{
		{
			Name:        "ping",
			Description: "You'll never believe what the command does!",
		},
		{
			Name:        "new-project",
			Description: "Creates a new project.",

			// Worth noting the required options must always go before the optional ones due to Discord's Slash-commands API
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "project-name",
					Description: "A name for the new project",
					Required:    true,
				},
			},
		},
	}

	commandHandlers = map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate){
		"ping": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "pong!",
				},
			})
		},
		"new-project": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			// Acknowledge the interaction first to prevent timeout
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "Creating project...",
				},
			})

			// Extract project name from options
			options := i.ApplicationCommandData().Options
			optionMap := make(map[string]*discordgo.ApplicationCommandInteractionDataOption, len(options))
			for _, opt := range options {
				optionMap[opt.Name] = opt
			}

			var projectName string
			if opt, ok := optionMap["project-name"]; ok {
				projectName = opt.StringValue()
			} else {
				s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
					Content: "Error: Project name is required.",
				})
				return
			}

			guildID := i.GuildID

			// Create role with the project name
			mentionable := true
			role, err := s.GuildRoleCreate(guildID, &discordgo.RoleParams{
				Name:        projectName,
				Mentionable: &mentionable,
			})
			if err != nil {
				s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
					Content: "Failed to create role: " + err.Error(),
				})
				return
			}

			if err != nil {
				s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
					Content: "Failed to update role: " + err.Error(),
				})
				return
			}

			// Create category
			category, err := s.GuildChannelCreateComplex(guildID, discordgo.GuildChannelCreateData{
				Name: projectName,
				Type: discordgo.ChannelTypeGuildCategory,
				PermissionOverwrites: []*discordgo.PermissionOverwrite{
					{
						ID:    role.ID,
						Type:  discordgo.PermissionOverwriteTypeRole,
						Allow: discordgo.PermissionViewChannel,
					},
					{
						ID:   guildID, // @everyone role has same ID as guild
						Type: discordgo.PermissionOverwriteTypeRole,
						Deny: discordgo.PermissionViewChannel,
					},
				},
			})
			if err != nil {
				s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
					Content: "Failed to create category: " + err.Error(),
				})
				return
			}

			// Create text channel "main" in the category
			_, err = s.GuildChannelCreateComplex(guildID, discordgo.GuildChannelCreateData{
				Name:     "main",
				Type:     discordgo.ChannelTypeGuildText,
				ParentID: category.ID,
			})
			if err != nil {
				s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
					Content: "Failed to create text channel: " + err.Error(),
				})
				return
			}

			// Create voice channel "Huddle" in the category
			_, err = s.GuildChannelCreateComplex(guildID, discordgo.GuildChannelCreateData{
				Name:     "Huddle",
				Type:     discordgo.ChannelTypeGuildVoice,
				ParentID: category.ID,
			})
			if err != nil {
				s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
					Content: "Failed to create voice channel: " + err.Error(),
				})
				return
			}

			// Send success message
			s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
				Content: fmt.Sprintf("Successfully created project **%s** with:\n- Category %s\n- Text channel #main\n- Voice channel Huddle\n- Role @%s",
					projectName, projectName, projectName),
			})
		},
	}
)

func init() {
	s.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if h, ok := commandHandlers[i.ApplicationCommandData().Name]; ok {
			h(s, i)
		}
	})
}

func main() {
	s.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log.Printf("Logged in as: %v#%v", s.State.User.Username, s.State.User.Discriminator)
	})
	err := s.Open()
	if err != nil {
		log.Fatalf("Cannot open the session: %v", err)
	}

	log.Println("Adding commands...")
	registeredCommands := make([]*discordgo.ApplicationCommand, len(commands))
	for i, v := range commands {
		cmd, err := s.ApplicationCommandCreate(s.State.User.ID, *GuildID, v)
		if err != nil {
			log.Panicf("Cannot create '%v' command: %v", v.Name, err)
		}
		registeredCommands[i] = cmd
	}

	defer s.Close()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	log.Println("Press Ctrl+C to exit")
	<-stop

	if *RemoveCommands {
		log.Println("Removing commands...")
		// // We need to fetch the commands, since deleting requires the command ID.
		// // We are doing this from the returned commands on line 375, because using
		// // this will delete all the commands, which might not be desirable, so we
		// // are deleting only the commands that we added.
		// registeredCommands, err := s.ApplicationCommands(s.State.User.ID, *GuildID)
		// if err != nil {
		// 	log.Fatalf("Could not fetch registered commands: %v", err)
		// }

		for _, v := range registeredCommands {
			err := s.ApplicationCommandDelete(s.State.User.ID, *GuildID, v.ID)
			if err != nil {
				log.Panicf("Cannot delete '%v' command: %v", v.Name, err)
			}
		}
	}

	log.Println("Gracefully shutting down.")
}
