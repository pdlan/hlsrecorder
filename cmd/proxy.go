package cmd

import (
	"log"
	"os"
	"time"
	"sync"
	"strings"
	"path"
	"net/http"
	"net/url"
	"io/ioutil"
	"encoding/json"

	"github.com/spf13/cobra"

	"hlsrecorder/request"
)

type FileCache struct {
	Mutex sync.Mutex
	Files map[string]int
}

var m3u8URI string
var cookies string
var database *request.RequestDatabase
var fileCache *FileCache

func download(requests *request.RequestDatabase, currURI, uri string, needBody bool) ([]byte, int, error) {
	parsedCurrURI, err := url.Parse(currURI)
	if err != nil {
		return nil, -1, err
	}
	parsedURI, err := url.Parse(uri)
	if err != nil {
		return nil, -1, err
	}
	var downloadURI string
	if parsedURI.Scheme == "" && parsedURI.Host == "" {
		parsedCurrURI.Path = path.Join(path.Dir(parsedCurrURI.Path), uri)
		downloadURI = parsedCurrURI.String()
	} else {
		downloadURI = uri
	}
	if idx, ok := fileCache.Files[downloadURI]; ok {
		var body []byte = nil
		if needBody {
			body = requests.ReadBody(idx)
		}
		return body, idx, nil
	} 
	log.Printf("download: %s", uri)
	req, err := http.NewRequest(http.MethodGet, downloadURI, nil)
	if err != nil {
		return nil, -1, err
	}
	if cookies != "" {
		req.Header.Set("Cookie", cookies)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, -1, err
	}
	noCache := false
	header := res.Header.Get("Cache-Control")
	if strings.Contains(header, "no-cache") {
		noCache = true
	}
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, -1, err
	}
	id := randomHex(16)
	metadata := request.Metadata {
		Host: req.Host,
		URI: downloadURI,
		Time: time.Now().UnixMicro(),
		Id: id,
	}
	filename := fileDir + "/" + id
	err = os.WriteFile(filename, body, 0644)
	if err != nil {
		log.Printf("Warning: failed to save %s: %s", downloadURI, err)
		return nil, -1, err
	}
	metadataEncoder.Encode(metadata)
	idx := requests.AddRequest(metadata)
	if !noCache {
		fileCache.Mutex.Lock()
		fileCache.Files[downloadURI] = idx
		fileCache.Mutex.Unlock()
	}
	return body, idx, nil
}

func updateProxiedPlaylist() error {
	last := time.Now().UnixMicro()
	for {
		playlist, err := request.LoadRemotePlaylist(database, download, m3u8URI)
		if playlist != nil {
			mutex.Lock()
			currentPlaylist = playlist
			mutex.Unlock()
		}
		if err != nil {
			log.Printf("Warning: failed to load playlist: %s", err)
		}
		now := time.Now().UnixMicro()
		diff := now - last
		last = now
		if diff < 1000000 {
			time.Sleep(time.Duration(1000000 - diff) * time.Microsecond)
		}
	}
}

func proxy(cmd *cobra.Command, args []string) {
	fileDir, _ = cmd.Flags().GetString("filedir")
	metadata, _ := cmd.Flags().GetString("metadata")
	listen, _ := cmd.Flags().GetString("listen")
	cookies, _ = cmd.Flags().GetString("cookies")
	m3u8URI = args[0]
	os.Mkdir(fileDir, 0755)

	metadataFile, err := os.OpenFile(metadata, os.O_APPEND | os.O_WRONLY | os.O_CREATE, 0600)
	if err != nil {
		log.Fatal(err)
	}
	metadataEncoder = json.NewEncoder(metadataFile)
	database = request.NewRequestDatabase(fileDir) 
	fileCache = &FileCache {
		Files: make(map[string]int),
	}
	go updateProxiedPlaylist()

	http.HandleFunc("/", fileHandler)
	err = http.ListenAndServe(listen, nil)
	if err != nil {
		log.Fatal(err)
	}
}

// proxyCmd represents the proxy command
var proxyCmd = &cobra.Command{
	Use:   "proxy",
	Short: "Record and proxy HLS live streaming automatically with a URI",
	Args: cobra.MinimumNArgs(1),
	Run: proxy,
}

func init() {
	rootCmd.AddCommand(proxyCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// proxyCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// proxyCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	proxyCmd.Flags().String("uri", "", "m3u8 URI")
	proxyCmd.Flags().String("cookies", "", "cookies for sending requests")
}
