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
	"juicehammer/juicebox"
	"log"
	"net/http"
	"os"
	"path"
	"sync"

	_ "golang.org/x/image/webp"

	"github.com/bwmarrin/discordgo"
	"github.com/corona10/goimagehash"
)

type KnownHash struct {
	hash        *goimagehash.ImageHash
	description string
}

type KnownHashes struct {
	sync.RWMutex
	hashes []KnownHash
}

var knownHashes KnownHashes = KnownHashes{hashes: make([]KnownHash, 0)}

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

	knownHashes.Lock()
	knownHashes.hashes = append(knownHashes.hashes, KnownHash{hash: hash, description: description})
	knownHashes.Unlock()

	return nil
}

var imgDir = "images"

func HashFolderImgs() error {
	subDirs, err := os.ReadDir(imgDir)
	if err != nil {
		return err
	}

	for _, subDir := range subDirs {
		// Only read subdirs
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

func ContributorPfp(url, description string) {
	resp, err := http.Get(url)
	if err != nil {
		log.Printf("Error getting image: %v\n", err)
	}
	defer resp.Body.Close()
	if err := pHashImg(resp.Body, description); err != nil {
		log.Printf("Error hashing image '%s' from url '%s': %v\n", description, url, err)
	}
}

func checkPfp(mem *discordgo.Member) (suspicious bool, msg string, err error) {
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

	knownHashes.RLock()
	defer knownHashes.RUnlock()
	for _, knownHash := range knownHashes.hashes {
		distance, err := knownHash.hash.Distance(hash)
		if err != nil {
			log.Printf("Error calculating distance for user %s: %v\n", mem.User.ID, err)
			continue
		}

		if distance < 10 {
			suspicious = true
			msg = fmt.Sprintf("%s has a suspicious PFP. Matches '%s' with distance %d.\nLink: %s", mem.Mention(), knownHash.description, distance, avatarUrl)
		}
	}

	return
}

func CheckAll(s *discordgo.Session) {
	var after string
	var wg sync.WaitGroup

	for {
		mems, err := s.GuildMembers(juicebox.JuiceboxGuildId, after, 1000)
		if err != nil {
			log.Fatalf("Error getting guild members: %s\n", err)
		}

	memLoop:
		for _, mem := range mems {
			// If the user is a contributor or admin, skip them
			for _, r := range mem.Roles {
				if r == juicebox.ContributorRoleId || r == juicebox.AdminRoleId {
					continue memLoop
				}
			}

			wg.Add(1)
			go func() {
				defer wg.Done()
				suspicious, msg, err := checkPfp(mem)

				if err != nil {
					log.Printf("Error checking PFP for user %s: %v\n", mem.User.ID, err)
					return
				}

				if suspicious {
					log.Println(msg + "\n")
				}
			}()
		}

		// If we get less than 1000 members, we're done
		if len(mems) < 1000 {
			break
		}

		// Update the after ID for the next iteration
		after = mems[len(mems)-1].User.ID
	}

	wg.Wait()
}
