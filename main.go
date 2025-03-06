package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"

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
	err := godotenv.Load()
	if err != nil {
		log.Println("Warning: Error loading .env file:", err)
	}

	flag.Parse()

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
						ID:   guildID, // good to know: @everyone role has same ID as guild
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

			// Add the role to onboarding
			onboarding, err := s.GuildOnboarding(guildID)
			if err != nil {
				s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
					Content: fmt.Sprintf("Successfully created project structures, but failed to get onboarding: %s", err.Error()),
				})
				return
			}

			var updatedPrompts []string
			var debugInfo []string

			rolePromptTitle := os.Getenv("NEWPROJECT_ONBOARDING_PROMPT_TITLE")
			visibilityPromptTitle := os.Getenv("NEWPROJECT_ONBOARDING_VISIBILITY_PROMPT_TITLE")

			debugInfo = append(debugInfo, fmt.Sprintf("Looking for prompts: '%s' and '%s'", rolePromptTitle, visibilityPromptTitle))
			debugInfo = append(debugInfo, fmt.Sprintf("Found %d prompts in onboarding", len(*onboarding.Prompts)))

			// Process all prompts
			for i := range *onboarding.Prompts {
				prompt := &(*onboarding.Prompts)[i]

				debugInfo = append(debugInfo, fmt.Sprintf("Checking prompt: '%s'", prompt.Title))

				// Add to role prompt
				if prompt.Title == rolePromptTitle {
					debugInfo = append(debugInfo, fmt.Sprintf("Found role prompt: '%s'", prompt.Title))

					newRoleOption := discordgo.GuildOnboardingPromptOption{
						Title:       projectName,
						Description: fmt.Sprintf("Join the %s project team", projectName),
						RoleIDs:     []string{role.ID},
					}
					prompt.Options = append(prompt.Options, newRoleOption)
					updatedPrompts = append(updatedPrompts, rolePromptTitle)
				}

				// Add to visibility prompt
				if prompt.Title == visibilityPromptTitle {
					debugInfo = append(debugInfo, fmt.Sprintf("Found visibility prompt: '%s'", prompt.Title))

					newVisibilityOption := discordgo.GuildOnboardingPromptOption{
						Title:       projectName,
						Description: fmt.Sprintf("See %s project channels", projectName),
						ChannelIDs:  []string{category.ID},
						RoleIDs:     []string{role.ID},
					}
					prompt.Options = append(prompt.Options, newVisibilityOption)
					updatedPrompts = append(updatedPrompts, visibilityPromptTitle)
				}
			}

			// Update onboarding if any prompts were modified
			if len(updatedPrompts) > 0 {
				debugInfo = append(debugInfo, fmt.Sprintf("Updating %d prompts: %s", len(updatedPrompts), strings.Join(updatedPrompts, ", ")))

				onboardingUpdate := discordgo.GuildOnboarding{
					Prompts:           onboarding.Prompts,
					Enabled:           onboarding.Enabled,
					DefaultChannelIDs: onboarding.DefaultChannelIDs,
					Mode:              onboarding.Mode,
				}

				_, err = s.GuildOnboardingEdit(guildID, &onboardingUpdate)

				if err != nil {
					s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
						Content: fmt.Sprintf("Successfully created project structures, but failed to update onboarding: %s\n\nDebug info:\n%s",
							err.Error(), strings.Join(debugInfo, "\n")),
					})
					return
				}

				s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
					Content: fmt.Sprintf("Successfully created project **%s** with:\n- Category %s\n- Text channel #main\n- Voice channel Huddle\n- Role @%s\n- Added to onboarding prompts: %s\n\nDebug info:\n%s",
						projectName, projectName, projectName, strings.Join(updatedPrompts, ", "), strings.Join(debugInfo, "\n")),
				})
			} else {
				// No prompts were updated
				s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
					Content: fmt.Sprintf("Successfully created project **%s** with:\n- Category %s\n- Text channel #main\n- Voice channel Huddle\n- Role @%s\nNote: No onboarding prompts were updated.\n\nDebug info:\n%s",
						projectName, projectName, projectName, strings.Join(debugInfo, "\n")),
				})
			}
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

		for _, v := range registeredCommands {
			err := s.ApplicationCommandDelete(s.State.User.ID, *GuildID, v.ID)
			if err != nil {
				log.Panicf("Cannot delete '%v' command: %v", v.Name, err)
			}
		}
	}

	log.Println("Bajabongo i leszczyny!")
}
