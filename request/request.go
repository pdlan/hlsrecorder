package request

import (
	"os"
	"fmt"
	"bufio"
	"bytes"
	"strings"
	"path"
	"regexp"
	"io/ioutil"
	"encoding/json"
	"net/url"

	"github.com/grafov/m3u8"
)

type Metadata struct {
	Host string `json:"host"`
	URI string `json:"uri"`
	Time int64 `json:"time"`
	Id string `json:"id"`
	Status int `json:"status"`
	Location string `json:"location"`
}

type RequestDatabase struct {
	Requests []Metadata
	FileDir string
}

type Playlist struct {
	Database *RequestDatabase
	Files map[string]int
	Index int
	M3U8Playlist *m3u8.MediaPlaylist
	M3U8File string
	M3U8SeqNo uint64
}

func ReadMetadata(filename, fileDir string) (*RequestDatabase, error) {
	var data []Metadata
	metadataFile, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer metadataFile.Close()

	scanner := bufio.NewScanner(metadataFile)
	for scanner.Scan() {
		line := scanner.Text()
		var d Metadata
		err := json.Unmarshal([]byte(line), &d)
		if err != nil {
			return nil, err
		}
		data = append(data, d)
	}
	return &RequestDatabase {
		Requests : data,
		FileDir: fileDir,
	}, nil
}

func NewRequestDatabase(fileDir string) *RequestDatabase {
	return &RequestDatabase {
		FileDir: fileDir,
	}
}

func (r *RequestDatabase) AddRequest(metadata Metadata) int {
	idx := len(r.Requests)
	r.Requests = append(r.Requests, metadata)
	return idx
}

func (r *RequestDatabase) FindRequest(idx int, pattern string) int {
	re := regexp.MustCompile(pattern)
	for i := idx; i < len(r.Requests); i++ {
		if re.MatchString(r.Requests[i].URI) {
			return i
		}
	}
	return -1
}

func (r *RequestDatabase) FindRequestContains(idx int, pattern string) int {
	for i := idx; i < len(r.Requests); i++ {
		if strings.Contains(r.Requests[i].URI, pattern) {
			return i
		}
	}
	return -1
}

func (r *RequestDatabase) FindRequestReverse(idx int, pattern string) int {
	re := regexp.MustCompile(pattern)
	if idx == -1 {
		idx = len(r.Requests) - 1
	}
	for i := idx; i >= 0; i-- {
		if re.MatchString(r.Requests[i].URI) {
			return i
		}
	}
	return -1
}

func (r *RequestDatabase) FindTimestamp(idx int, timestamp int64) int {
	for i := idx; i < len(r.Requests); i++ {
		if r.Requests[i].Time > timestamp && i - 1 >= idx {
			return i - 1
		}
	}
	return -1
}

func (r *RequestDatabase) ReadBody(idx int) []byte {
	filename := r.FileDir + "/" + r.Requests[idx].Id
	data, _ := ioutil.ReadFile(filename)
	return data
}

func (r *RequestDatabase) HasFile(idx int) bool {
	filename := r.FileDir + "/" + r.Requests[idx].Id
	_, err := os.Stat(filename)
	return !os.IsNotExist(err)
}

func LoadPlaylist(requests *RequestDatabase, idx int,
					reverse bool, timestamp int64) (*Playlist, int, error) {
	if timestamp != -1 {
		idx = requests.FindTimestamp(0, timestamp)
	}
	var m3u8Idx int
	if reverse {
		m3u8Idx = requests.FindRequestReverse(idx, ".*\\.m3u8(\\?.*)?$")
	} else {
		m3u8Idx = requests.FindRequest(idx, ".*\\.m3u8(\\?.*)?$")
	}
	if m3u8Idx == -1 {
		return nil, -1, nil
	}
	m3u8File := requests.ReadBody(m3u8Idx)
	buffer := bytes.NewBuffer(m3u8File)
	p, listType, err := m3u8.Decode(*buffer, false)
	if err != nil {
		return nil, m3u8Idx, err
	}
	if listType != m3u8.MEDIA {
		return nil, m3u8Idx, fmt.Errorf("m3u8 is not media list")
	}
	mediaPlaylist := p.(*m3u8.MediaPlaylist)
	playlist := &Playlist {
		Database: requests,
		Files: make(map[string]int),
		Index: m3u8Idx,
		M3U8Playlist: mediaPlaylist,
		M3U8File: mediaPlaylist.String(),
		M3U8SeqNo: mediaPlaylist.SeqNo,
	}
	if mediaPlaylist.Key != nil {
		filename, _, err := playlist.FindOrSetURI(mediaPlaylist.Key.URI)
		if err != nil {
			return nil, m3u8Idx, err
		}
		mediaPlaylist.Key.URI = filename
	}
	for _, segment := range mediaPlaylist.Segments {
		if segment == nil {
			continue
		}
		filename, _, err := playlist.FindOrSetURI(segment.URI)
		if err != nil {
			return nil, m3u8Idx, err
		}
		segment.URI = filename
		if segment.Key != nil {
			filename, _, err = playlist.FindOrSetURI(segment.Key.URI)
			if err != nil {
				return nil, m3u8Idx, err
			}
			segment.Key.URI = filename
		}
	}
	return playlist, m3u8Idx, nil
}

type DownloadFunction func (requests *RequestDatabase, currURI, uri string, needBody bool) ([]byte, int, error)

func LoadRemotePlaylist(requests *RequestDatabase, downloadFunc DownloadFunction,
							uri string) (*Playlist, error) {
	m3u8File, m3u8Idx, err := downloadFunc(requests, "", uri, true)
	if err != nil {
		return nil, err
	}
	p, listType, err := m3u8.Decode(*bytes.NewBuffer(m3u8File), false)
	if err != nil {
		return nil, err
	}
	if listType != m3u8.MEDIA {
		return nil, fmt.Errorf("m3u8 is not media list")
	}
	mediaPlaylist := p.(*m3u8.MediaPlaylist)
	playlist := &Playlist {
		Database: requests,
		Files: make(map[string]int),
		Index: m3u8Idx,
		M3U8Playlist: mediaPlaylist,
		M3U8File: mediaPlaylist.String(),
		M3U8SeqNo: mediaPlaylist.SeqNo,
	}
	if mediaPlaylist.Key != nil {
		filename, err := playlist.FindOrDownloadURI(downloadFunc, uri, mediaPlaylist.Key.URI)
		if err != nil {
			return nil, err
		}
		mediaPlaylist.Key.URI = filename
	}
	for _, segment := range mediaPlaylist.Segments {
		if segment == nil {
			continue
		}
		filename, err := playlist.FindOrDownloadURI(downloadFunc, uri, segment.URI)
		if err != nil {
			return nil, err
		}
		segment.URI = filename
		if segment.Key != nil {
			filename, err = playlist.FindOrDownloadURI(downloadFunc, uri, segment.Key.URI)
			if err != nil {
				return nil, err
			}
			segment.Key.URI = filename
		}
	}
	return playlist, nil
}

func (p *Playlist) FindOrSetURI(uri string) (string, int, error) {
	parsedURI, err := url.Parse(uri)
	if err != nil {
		return "", -1, err
	}
	var filename string
	if parsedURI.Scheme == "" && parsedURI.Host == "" {
		filename = path.Base(uri)
	} else {
		filename = path.Base(parsedURI.Path)
	}
	idx, ok := p.Files[filename]
	if ok {
		return filename, idx, nil
	}
	idx = p.Database.FindRequestContains(0, filename)
	if idx == -1 || !p.Database.HasFile(idx) {
		return "", -1, fmt.Errorf("failed to find file")
	}
	p.Files[filename] = idx
	return filename, idx, nil
}

func (p *Playlist) FindOrDownloadURI(downloadaFunc DownloadFunction,
										currURI, uri string) (string, error) {
	parsedURI, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	var filename string
	if parsedURI.Scheme == "" && parsedURI.Host == "" {
		filename = path.Base(uri)
	} else {
		filename = path.Base(parsedURI.Path)
	}
	_, ok := p.Files[filename]
	if ok {
		return filename, nil
	}
	_, idx, err := downloadaFunc(p.Database, currURI, uri, false)
	if err != nil {
		return "", err
	}
	p.Files[filename] = idx
	return filename, nil
}

func (p *Playlist) ReadFile(filename string) []byte {
	idx, ok := p.Files[filename]
	if !ok || idx == -1 {
		return nil
	}
	return p.Database.ReadBody(idx)
}
