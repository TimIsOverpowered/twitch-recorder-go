package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	Platform "twitch-recorder-go/platform"
	utils "twitch-recorder-go/utils"

	"golang.org/x/oauth2"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

func main() {
	var wg sync.WaitGroup
	wg.Add(1)
	for _, channel := range utils.Config.Channels {
		if channel.Platform == "twitch" {
			if !Platform.TwitchCheckIfUserExists(channel.Name) {
				log.Printf("[Twitch] %s does not exist", channel)
				time.Sleep(500 * time.Millisecond)
				continue
			}
			go TwitchInterval(channel.Name, nil)
		}
		if channel.Platform == "kick" {
			if !Platform.KickCheckIfUserExists(channel.Name) {
				log.Printf("[Kick] %s does not exist", channel)
				time.Sleep(500 * time.Millisecond)
				continue
			}
			go KickInterval(channel.Name, nil)
		}
	}
	wg.Wait()
}

func twitchCheck(channel string, token *Platform.TwitchTokenSig) {
	m3u8, live := Platform.TwitchCheckIfLive(token, channel)
	if live {
		err := record(m3u8, channel, "twitch")
		if err != nil {
			log.Printf("[Twitch] [%s] %v", channel, err)
		}
	}
}

func TwitchInterval(channel string, token *Platform.TwitchTokenSig) {
	for token == nil {
		var err error
		token, err = Platform.TwitchGetLiveTokenSig(channel)
		if err != nil {
			log.Printf("[Kick] [%s] %v", channel, err)
			time.Sleep(5 * time.Second)
		}
	}

	var value *Platform.TwitchValue

	if err := json.Unmarshal([]byte(token.Data.Token.Value), &value); err != nil {
		log.Printf("[Twitch] [%s] Something went wrong trying to get token sig expiration.. %v", channel, err)
		time.AfterFunc(6*time.Second, func() {
			TwitchInterval(channel, nil)
		})
		return
	}

	if time.Now().Unix() >= value.Expires {
		log.Printf("[Twitch] [%s] Live Token Sig Expired...", channel)
		time.AfterFunc(6*time.Second, func() {
			TwitchInterval(channel, nil)
		})
		return
	}

	twitchCheck(channel, token)

	time.AfterFunc(6*time.Second, func() {
		TwitchInterval(channel, token)
	})
}

func KickInterval(channel string, kickChannelObject *Platform.KickChannelStruct) {
	for kickChannelObject == nil {
		var err error
		kickChannelObject, err = Platform.KickGetChannel(channel)
		if err != nil {
			log.Printf("[%s] %v", channel, err)
			time.Sleep(5 * time.Second)
		}
	}

	if time.Now().Unix() >= kickChannelObject.Token.Expires {
		log.Printf("[Kick] [%s] Live Token Sig Expired...", channel)
		time.AfterFunc(6*time.Second, func() {
			KickInterval(channel, nil)
		})
		return
	}

	kickCheck(channel, kickChannelObject)

	time.AfterFunc(6*time.Second, func() {
		KickInterval(channel, kickChannelObject)
	})
}

func kickCheck(channel string, kickChannelObject *Platform.KickChannelStruct) {
	m3u8, live := Platform.KickCheckIfLive(kickChannelObject, channel)
	if live {
		err := record(m3u8, channel, "kick")
		if err != nil {
			log.Printf("[Kick] [%s] %v", channel, err)
		}
	}
}

func record(m3u8 string, channel string, platform string) error {
	date := time.Now().Format("01-02-2006")
	log.Printf("[%s] [%s] is live. %s", platform, channel, date)
	var path string
	if runtime.GOOS == "windows" {
		path = utils.Config.Vod_directory + "\\" + channel + "\\"
	} else {
		path = utils.Config.Vod_directory + "/" + channel + "/"
	}
	if !utils.FileExists(path) {
		os.MkdirAll(path, 0777)
	}
	fileName := date + "_" + platform + ".mp4"

	if utils.FileExists(path + fileName) {
		os.Remove(path + fileName)
	}

	if platform == "twitch" {
		var stream *Platform.TwitchStreams
		var err error

		go func() {
			stream, err = Platform.TwitchGetStreamObject(channel)
			if err != nil {
				log.Printf("[%s] [%s] %v", platform, channel, err)
			}
			for len(stream.StreamsData) == 0 {
				stream, err = Platform.TwitchGetStreamObject(channel)
				if err != nil {
					log.Printf("[%s] [%s] %v", platform, channel, err)
				}
				time.Sleep(5 * time.Second)
			}
		}()

		if !utils.USE_FFMPEG {
			//use streamlink
			cmd := exec.Command("streamlink", "-o", path+fileName, "twitch.tv/"+channel, "best", "--twitch-disable-hosting", "--twitch-disable-ads", "--twitch-disable-reruns")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Run()
		} else {
			//use ffmpeg
			log.Printf("[%s] [%s] Executing ffmpeg: %s", platform, channel, "ffmpeg -y -i "+m3u8+" -c copy -copyts -start_at_zero -bsf:a aac_adtstoasc -f mp4 "+path+fileName)
			cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "warning", "-y", "-rw_timeout", "3000000", "-i", m3u8, "-c", "copy", "-copyts", "-start_at_zero", "-bsf:a", "aac_adtstoasc", "-f", "mp4", path+fileName)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Run()
			log.Printf("[%s] [%s] Finished downloading.. Saved at: %s", platform, channel, path+fileName)
		}

		if len(stream.StreamsData) == 0 {
			return errors.New(channel + "'s stream object not found..")
		}

		new_fileName := stream.StreamsData[0].Id + platform + ".mp4"
		e := os.Rename(path+fileName, path+new_fileName)
		if e != nil {
			return err
		}

		if utils.UPLOAD_TO_DRIVE {
			go func() {
				err := uploadToDrive(path, new_fileName, channel, stream.StreamsData[0].Id, platform)
				if err != nil {
					log.Printf("[%s] %v", channel, err)
				}
			}()
		}
	}

	return nil
}

func uploadToDrive(path string, fileName string, channel string, streamId string, platform string) error {
	if !utils.FileExists(path + fileName) {
		return errors.New("File does not exist.. do not upload")
	}
	log.Printf("[%s] [%s] Uploading to drive..", platform, channel)
	//upload to gdrive
	ctx := context.Background()
	var googleConfig oauth2.Config
	googleConfig.ClientID = utils.Config.Google.ClientId
	googleConfig.ClientSecret = utils.Config.Google.ClientSecret
	googleConfig.Endpoint.TokenURL = utils.Config.Google.Endpoint.TokenURL
	googleConfig.Scopes = utils.Config.Google.Scopes
	client, err := getClient(&googleConfig)
	if err != nil {
		return err
	}

	srv, err := drive.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return err
	}

	//Retrieve Folder Id using channel name.
	nextPageToken := ""
	fileList, err := getDriveFileList(srv, nextPageToken)
	if err != nil {
		return err
	}
	rootFolderId := ""
	streamIdFolder := ""
	for {
		for _, file := range fileList.Files {
			if strings.EqualFold(file.Name, channel) {
				log.Printf("[%s] [%s] Found root folder %s", platform, channel, file.Id)
				rootFolderId = file.Id
				continue
			}
			if strings.EqualFold(file.Name, streamId) {
				log.Printf("[%s] [%s] Found stream id folder %s", platform, channel, file.Id)
				streamIdFolder = file.Id
				continue
			}
			if len(rootFolderId) > 0 && len(streamIdFolder) > 0 {
				break
			}
		}
		nextPageToken = fileList.NextPageToken
		fileList, err = getDriveFileList(srv, nextPageToken)
		if fileList.NextPageToken == "" || (len(rootFolderId) > 0 && len(streamIdFolder) > 0) {
			break
		}
	}

	//Create root folder if it doesn't exist.
	if len(rootFolderId) == 0 {
		log.Printf("[%s] [%s] Creating root folder", platform, channel)
		res, err := srv.Files.Create(&drive.File{Name: channel, MimeType: "application/vnd.google-apps.folder"}).Do()
		if err != nil {
			return err
		}
		rootFolderId = res.Id
	}

	if len(streamIdFolder) == 0 {
		//Create Stream Id Folder if it doesn't exist
		log.Printf("[%s] [%s] Creating %s folder", platform, channel, streamId)
		res, err := srv.Files.Create(&drive.File{Name: streamId, MimeType: "application/vnd.google-apps.folder", Parents: []string{rootFolderId}}).Do()
		if err != nil {
			return err
		}
		streamIdFolder = res.Id
	}

	//Upload FIle to Drive
	log.Printf("[%s] [%s] Uploading file", platform, channel)
	f, err := os.Open(path + fileName)
	if err != nil {
		return err
	}
	defer f.Close()
	// Grab file info
	inputInfo, err := f.Stat()
	if err != nil {
		return err
	}
	getRate := MeasureTransferRate()

	// progress call back
	showProgress := func(current, total int64) {
		fmt.Printf("Uploaded at %s, %s/%s\r", getRate(current), Comma(current), Comma(total))
	}

	res, err := srv.Files.Create(&drive.File{Name: fileName, Parents: []string{streamIdFolder}}).ResumableMedia(context.Background(), f, inputInfo.Size(), mime.TypeByExtension(filepath.Ext(fileName))).ProgressUpdater(showProgress).Do()
	if err != nil {
		return err
	}
	log.Printf("[%s] [%s] Uploaded %s Drive Id: %s", platform, channel, res.Name, res.Id)

	return nil
}

func Comma(v int64) string {
	sign := ""
	if v < 0 {
		sign = "-"
		v = 0 - v
	}

	parts := []string{"", "", "", "", "", "", ""}
	j := len(parts) - 1

	for v > 999 {
		parts[j] = strconv.FormatInt(v%1000, 10)
		switch len(parts[j]) {
		case 2:
			parts[j] = "0" + parts[j]
		case 1:
			parts[j] = "00" + parts[j]
		}
		v = v / 1000
		j--
	}
	parts[j] = strconv.Itoa(int(v))
	return sign + strings.Join(parts[j:], ",")
}

func FileSizeFormat(bytes int64, forceBytes bool) string {
	if forceBytes {
		return fmt.Sprintf("%v B", bytes)
	}

	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}

	var i int
	value := float64(bytes)

	for value > 1000 {
		value /= 1000
		i++
	}
	return fmt.Sprintf("%.1f %s", value, units[i])
}

func MeasureTransferRate() func(int64) string {
	start := time.Now()

	return func(bytes int64) string {
		seconds := int64(time.Now().Sub(start).Seconds())
		if seconds < 1 {
			return fmt.Sprintf("%s/s", FileSizeFormat(bytes, false))
		}
		bps := bytes / seconds
		return fmt.Sprintf("%s/s", FileSizeFormat(bps, false))
	}
}

func getClient(c *oauth2.Config) (*http.Client, error) {
	var tok oauth2.Token
	tok.AccessToken = utils.Config.Drive.Access_Token
	tok.RefreshToken = utils.Config.Drive.Refresh_Token
	tok.TokenType = utils.Config.Drive.TokenType
	tok.Expiry = utils.Config.Drive.Expiry
	tokenSource := c.TokenSource(oauth2.NoContext, &tok)
	newToken, err := tokenSource.Token()
	if err != nil {
		return nil, err
	}
	if newToken.AccessToken != tok.AccessToken {
		log.Println("Saving new drive tokens..")
		utils.Config.Drive.Access_Token = newToken.AccessToken
		utils.Config.Drive.Refresh_Token = newToken.RefreshToken
		utils.Config.Drive.TokenType = newToken.TokenType
		utils.Config.Drive.Expiry = newToken.Expiry
		d, err := json.MarshalIndent(utils.Config, "", " ")
		if err != nil {
			return nil, err
		}
		err = ioutil.WriteFile(utils.CfgPath, d, 0777)
		if err != nil {
			return nil, err
		}
	}
	return oauth2.NewClient(context.Background(), tokenSource), nil
}

func getDriveFileList(driveSvc *drive.Service, nextPageToken string) (*drive.FileList, error) {
	return driveSvc.Files.List().Fields("nextPageToken, files/*").PageToken(nextPageToken).Do()
}
