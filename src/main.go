package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/bwmarrin/dgvoice"
	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"
)

const (
	RADIO_FLAMINGO    string = "https://live.radioflamingo.at/rf"
	RADIO_BOLLERWAGEN string = "http://player.ffn.de/radiobollerwagen.mp3"
	CHANNELS          int    = 2
	FRAME_SIZE        int    = 960                       // Discord requires 20ms of audio = 960 samples @ 48kHz
	BYTE_SIZE         int    = FRAME_SIZE * CHANNELS * 2 // total bytes needed (2 bytes per int16)
)

var (
	logger     *zap.SugaredLogger
	voiceConns = make(map[string]*discordgo.VoiceConnection)
)

func main() {
	var err error
	var ok bool
	var token string
	var log *zap.Logger
	var discordClient *discordgo.Session

	if log, err = zap.NewProduction(); err != nil {
		panic("Failed to create logger: " + err.Error())
	}

	defer log.Sync()
	logger = log.Sugar()

	if token, ok = os.LookupEnv("TOKEN"); !ok || token == "" {
		logger.Fatal("Env variable TOKEN not set")
	}

	if discordClient, err = discordgo.New("Bot " + token); err != nil {
		logger.Fatal("Error creating Discord session", zap.Error(err))
	}

	discordClient.AddHandler(onMessageCreate)

	if err = discordClient.Open(); err != nil {
		logger.Fatal("Error opening Discord session", zap.Error(err))
	}

	logger.Info("Bot is now running. Press CTRL+C to exit.")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-stop

	logger.Info("Shutting down bot...")
	discordClient.Close()
}

func onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.Bot {
		return
	}

	switch m.Content {
	case "!bollerwagen":
		logger.Infow("Received !bollerwagen command", "user", m.Author.Username)
		joinAndPlay(s, m, RADIO_BOLLERWAGEN)
	case "!flamingo":
		logger.Infow("Received !flamingo command", "user", m.Author.Username)
		joinAndPlay(s, m, RADIO_FLAMINGO)
	case "!leave":
		logger.Infow("Received !leave command", "user", m.Author.Username)
		leaveChannel(s, m)
	}
}
func joinAndPlay(s *discordgo.Session, m *discordgo.MessageCreate, streamUrl string) (err error) {
	var guild *discordgo.Guild
	var userChannelID string
	var vc *discordgo.VoiceConnection

	if guild, err = s.State.Guild(m.GuildID); err != nil {
		err = fmt.Errorf("failed to get guild '%s': %v", m.GuildID, err)
		return
	}

	for _, vs := range guild.VoiceStates {
		if vs.UserID == m.Author.ID {
			userChannelID = vs.ChannelID
			break
		}
	}

	if userChannelID == "" {
		s.ChannelMessageSend(m.ChannelID, "You're not in a voice channel.")
		return
	}

	if vc, err = s.ChannelVoiceJoin(m.GuildID, userChannelID, false, true); err != nil {
		err = fmt.Errorf("failed to join voice channel: %v", err)
		s.ChannelMessageSend(m.ChannelID, "Failed to join the voice channel.")
		return
	}

	voiceConns[m.GuildID] = vc
	logger.Infow("Joined voice channel", "guildID", m.GuildID, "channelID", userChannelID)

	go streamAudio(vc, streamUrl)

	return
}

func leaveChannel(s *discordgo.Session, m *discordgo.MessageCreate) {
	if vc, ok := voiceConns[m.GuildID]; ok {
		vc.Disconnect()
		delete(voiceConns, m.GuildID)
		logger.Infow("Disconnected from voice channel", "guildID", m.GuildID)
		s.ChannelMessageSend(m.ChannelID, "Left the voice channel.")
	} else {
		logger.Warnw("Leave command received but not in a voice channel", "guildID", m.GuildID)
		s.ChannelMessageSend(m.ChannelID, "Not in a voice channel.")
	}
}

func streamAudio(vc *discordgo.VoiceConnection, streamUrl string) (err error) {
	var ffmpegArgs []string
	var cmd *exec.Cmd
	var stdout io.ReadCloser
	var buffer []byte
	var sendChan chan []int16

	vc.Speaking(true)
	defer vc.Speaking(false)

	ffmpegArgs = []string{
		"-i", streamUrl,
		"-f", "s16le",
		"-ar", "48000",
		"-ac", "2",
		"-loglevel", "quiet",
		"pipe:1",
	}

	cmd = exec.Command("ffmpeg", ffmpegArgs...)

	if stdout, err = cmd.StdoutPipe(); err != nil {
		err = fmt.Errorf("Failed to get ffmpeg stdout: %w", err)
		return
	}

	cmd.Stderr = os.Stderr

	if err = cmd.Start(); err != nil {
		err = fmt.Errorf("Failed to start ffmpeg: %w", err)
		return
	}

	buffer = make([]byte, BYTE_SIZE) // 2 channels * 960 samples * 2 bytes per sample
	sendChan = make(chan []int16)

	go dgvoice.SendPCM(vc, sendChan)

	for {
		var frame []int16
		var i int
		var j int

		if _, err = io.ReadFull(stdout, buffer); err != nil {
			close(sendChan)
			return
		}

		frame = make([]int16, CHANNELS*FRAME_SIZE)

		for i = 0; i < CHANNELS*FRAME_SIZE; i++ {
			j = i * 2
			frame[i] = int16(buffer[j]) | int16(buffer[j+1])<<8
		}

		sendChan <- frame
	}
}
