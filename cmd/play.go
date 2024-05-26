package cmd

import (
	"time"
	"sync"
	"net/http"
	"log"
	"strings"

	"github.com/spf13/cobra"

	"hlsrecorder/request"
)

var mutex sync.Mutex
var currentPlaylist *request.Playlist

func findLastPlaylist(metadata, fileDir string, timestamp int64) *request.Playlist {
	database, err := request.ReadMetadata(metadata, fileDir)
	if err != nil {
		return nil
	}
	idx := -1
	var playlist *request.Playlist
	for {
		playlist, idx, err = request.LoadPlaylist(database, idx, true, timestamp)
		if idx == -1 {
			break
		}
		if err == nil {
			return playlist
		}
		if idx == 0 {
			break
		}
		idx--
	}
	return nil
}

func updatePlaylist(realtime bool, offset int, metadata, fileDir string) error {
	var timestamp int64
	timestamp = -1
	if !realtime {
		database, err := request.ReadMetadata(metadata, fileDir)
		if err == nil && len(database.Requests) > 0 {
			timestamp = database.Requests[0].Time + int64(offset) * 1000000
		}
	}
	base := timestamp
	start := time.Now().UnixMicro()
	for {
		playlist := findLastPlaylist(metadata, fileDir, timestamp)
		time.Sleep(time.Second)
		diff := time.Now().UnixMicro() - start
		if !realtime {
			timestamp = base + diff
		}
		if playlist == nil {
			continue
		}
		mutex.Lock()
		if currentPlaylist == nil || currentPlaylist.M3U8SeqNo < playlist.M3U8SeqNo {
			currentPlaylist = playlist
		}
		mutex.Unlock()
	}
}

func fileHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.URL.Path, "/")
	var playlist *request.Playlist
	mutex.Lock()
	playlist = currentPlaylist
	mutex.Unlock()
	if playlist == nil {
		w.WriteHeader(404)
		return
	}
	if path == "play.m3u8" {
		w.Header().Set("Content-Type", "application/vnd")
		w.Write([]byte(playlist.M3U8File))
		return
	}
	body := playlist.ReadFile(path)
	if body == nil {
		w.WriteHeader(404)
		return
	}
	w.Write(body)
}

func play(cmd *cobra.Command, args []string) {
	realtime, _ := cmd.Flags().GetBool("realtime")
	fileDir, _ = cmd.Flags().GetString("filedir")
	metadata, _ := cmd.Flags().GetString("metadata")
	starttime, _ := cmd.Flags().GetInt("starttime")
	listen, _ := cmd.Flags().GetString("listen")
	go updatePlaylist(realtime, starttime, metadata, fileDir)

	http.HandleFunc("/", fileHandler)
	err := http.ListenAndServe(listen, nil)
	if err != nil {
		log.Fatal(err)
	}
}

// playCmd represents the play command
var playCmd = &cobra.Command{
	Use:   "play",
	Short: "Play the recorded live streaming",
	Run: play,
}

func init() {
	rootCmd.AddCommand(playCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// playCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// playCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	playCmd.Flags().Bool("realtime", false, "Play a live streaming that is being recorded")
	playCmd.Flags().Int("starttime", 0, "Seconds since the first timestamp after which playing starts.")
}
