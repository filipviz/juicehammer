// pfp checks profile pictures against a list of known profile pictures to prevent impersonation using perceptual hashing.
// See https://www.hackerfactor.com/blog/index.php?/archives/432-Looks-Like-It.html
package pfp

import (
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"sync"

	_ "golang.org/x/image/webp"

	"github.com/bwmarrin/discordgo"
	"github.com/corona10/goimagehash"
)

type Hash struct {
	hash        *goimagehash.ImageHash
	description string
}

type HashSlice struct {
	sync.RWMutex
	hashes []Hash
}

var monitoredHashes = HashSlice{hashes: make([]Hash, 0)}

// Perceptually hash an image and add it to the list of known hashes
func pHashImg(img io.ReadCloser, description string) error {
	defer img.Close()

	decoded, _, err := image.Decode(img)
	if err != nil {
		return err
	}
	hash, err := goimagehash.PerceptionHash(decoded)
	if err != nil {
		return err
	}

	monitoredHashes.Lock()
	monitoredHashes.hashes = append(monitoredHashes.hashes, Hash{hash, description})
	monitoredHashes.Unlock()

	return nil
}

var imgDir = "images"

// Hash all of the known suspicious images from the images directory.
func HashFolderImgs() error {
	subDirs, err := os.ReadDir(imgDir)
	if err != nil {
		return err
	}

	for _, subDir := range subDirs {
		// Skip files
		if !subDir.IsDir() {
			continue
		}

		subDirPath := path.Join(imgDir, subDir.Name())
		imgs, err := os.ReadDir(subDirPath)
		if err != nil {
			return err
		}

		for _, img := range imgs {
			imgPath := path.Join(subDirPath, img.Name())
			imgFile, err := os.Open(imgPath)
			if err != nil {
				return err
			}
			defer imgFile.Close()

			description := fmt.Sprintf("known image '%s'", img.Name())
			if err := pHashImg(imgFile, description); err != nil {
				log.Printf("Error hashing image '%s': %v\n", img.Name(), err)
			}
		}
	}

	return nil
}

func MonitorPfp(url, description string) {
	resp, err := http.Get(url)
	if err != nil {
		log.Printf("Error getting image: %v\n", err)
	}
	defer resp.Body.Close()
	if err := pHashImg(resp.Body, description); err != nil {
		log.Printf("Error hashing image '%s' from url '%s': %v\n", description, url, err)
	}
}

// Check whether a user's profile picture is suspicious.
// Returns true if the profile picture is suspicious, along with a message describing the match.
func PfpIsSuspicious(mem *discordgo.Member) (is bool, msg string, err error) {
	avatarUrl := mem.AvatarURL("256")
	if avatarUrl == "" {
		return false, "", fmt.Errorf("avatar url is empty")
	}

	resp, err := http.Get(avatarUrl)
	if err != nil {
		log.Printf("Error getting image for user %s: %v\n", mem.User.ID, err)
		return false, "", err
	}
	defer resp.Body.Close()

	decoded, _, err := image.Decode(resp.Body)
	if err != nil {
		log.Printf("Error decoding image '%s' for user %s: %v\n", avatarUrl, mem.User.ID, err)
		return false, "", err
	}

	hash, err := goimagehash.PerceptionHash(decoded)
	if err != nil {
		log.Printf("Error hashing image '%s' for user %s: %v\n", avatarUrl, mem.User.ID, err)
		return false, "", err
	}

	monitoredHashes.RLock()
	defer monitoredHashes.RUnlock()
	for _, knownHash := range monitoredHashes.hashes {
		distance, err := knownHash.hash.Distance(hash)
		if err != nil {
			log.Printf("Error calculating distance for user %s: %v\n", mem.User.ID, err)
			continue
		}

		if distance < 10 {
			is = true
			msg = fmt.Sprintf("%s has a suspicious PFP. Matches '%s' with distance %d.\nLink: %s", mem.Mention(), knownHash.description, distance, avatarUrl)
			break
		}
	}

	return
}
