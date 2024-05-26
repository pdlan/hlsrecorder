package cmd

import (
	"fmt"
	"log"
	"os"
	"io/ioutil"
	"encoding/hex"
	"crypto/aes"
    "crypto/cipher"

	"github.com/spf13/cobra"

	"hlsrecorder/request"
)

func loadIV(iv string) []byte {
	if len(iv) < 2 {
		return nil
	}
	iv = iv[2:]
	res, err := hex.DecodeString(iv)
	if err != nil {
		return nil
	}
	return res
}

func unpad(data []byte) ([]byte, error) {
    if len(data) == 0 {
        return nil, fmt.Errorf("invalid padding on empty data")
    }
    paddingLen := int(data[len(data)-1])
    if paddingLen > len(data) || paddingLen > aes.BlockSize {
        return nil, fmt.Errorf("invalid padding length")
    }
    return data[:len(data)-paddingLen], nil
}

func decryptAES128CBC(encryptedData, key, iv []byte) ([]byte, error) {
    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, err
    }

    if len(encryptedData) < aes.BlockSize || len(encryptedData)%aes.BlockSize != 0 {
        return nil, err
    }

    mode := cipher.NewCBCDecrypter(block, iv)
    decryptedData := make([]byte, len(encryptedData))
    mode.CryptBlocks(decryptedData, encryptedData)

    // Unpad the decrypted data
    decryptedData, err = unpad(decryptedData)
    if err != nil {
        return nil, err
    }

    return decryptedData, nil
}

func decryptFile(key, iv []byte, data []byte, ofile string) error {
	decrypted, err := decryptAES128CBC(data, key, iv)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(ofile, decrypted, 0644)
	if err != nil {
		return err
	}
	return nil
}

func dump(cmd *cobra.Command, args []string) {
	fileDir, _ = cmd.Flags().GetString("filedir")
	metadata, _ := cmd.Flags().GetString("metadata")
	outputDir, _ := cmd.Flags().GetString("outputdir")

	os.Mkdir(outputDir, 0755)

	database, err := request.ReadMetadata(metadata, fileDir)
	if err != nil {
		log.Fatal(err)
	}
	idx := 0
	processedFiles := make(map[string]bool)
	for {
		var playlist *request.Playlist
		var err error
		playlist, idx, err = request.LoadPlaylist(database, idx, false, -1)
		if idx == -1 {
			break
		}
		idx++
		if err != nil {
			fmt.Printf("failed to load playlist: %s\n", err);
			continue
		}
		m3u8Playlist := playlist.M3U8Playlist
		var defaultKey []byte = nil
		var defaultIV []byte = nil
		if m3u8Playlist.Key != nil {
			defaultKey = playlist.ReadFile(m3u8Playlist.Key.URI)
			defaultIV = loadIV(m3u8Playlist.Key.IV)
		}
		for _, segment := range m3u8Playlist.Segments {
			if segment == nil {
				continue
			}
			filename := segment.URI
			p, ok := processedFiles[filename]
			if p && ok {
				continue
			}
			data := playlist.ReadFile(filename)
			if data == nil {
				continue
			}
			var key []byte
			var iv []byte
			if segment.Key != nil {
				key = playlist.ReadFile(segment.Key.URI)
				iv = loadIV(segment.Key.IV)
			} else {
				key = defaultKey
				iv = defaultIV
			}
			if key != nil && iv != nil {
				err = decryptFile(key, iv, data, outputDir + "/" + filename)
			} else {
				err = ioutil.WriteFile(outputDir + "/" + filename, data, 0644)
			}
			if err != nil {
				fmt.Printf("Failed to decrypt %s: %s\n", filename, err)
			}
			processedFiles[filename] = true
		}
	}
}

// dumpCmd represents the dump command
var dumpCmd = &cobra.Command{
	Use:   "dump",
	Short: "Dump recorded live streaming to ts files",
	Run: dump,
}

func init() {
	rootCmd.AddCommand(dumpCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// dumpCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// dumpCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	dumpCmd.Flags().String("outputdir", "output/", "output dir")
}
