package platform

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"

	"github.com/go-resty/resty/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/grafov/m3u8"
)

const (
	KICK_API_BASE = "https://kick.com/api/v2"
)

type KickChannelStruct struct {
	Id          string `json:"id"`
	UserId      string `json:"user_id"`
	PlaybackUrl string `json:"playback_url"`
	Token       KickToken
}

type KickToken struct {
	Token   string `json:"token"`
	Expires int64  `json:"exp"`
}

func KickGetChannel(channel string) (*KickChannelStruct, error) {
	log.Printf("[Kick] [%s] Getting Kick Channel Object", channel)
	client := resty.New()

	resp, _ := client.R().
		SetHeader("Accept", "application/json").
		Get(KICK_API_BASE + "/channels/" + channel)

	if resp.StatusCode() != 200 {
		log.Printf("Unexpected status code, expected %d, got %d instead", 200, resp.StatusCode())
		return nil, errors.New("Something went wrong retrieving channel object from kick")
	} else {
		var channel KickChannelStruct
		err := json.Unmarshal(resp.Body(), &channel)
		if err != nil {
			return nil, err
		}

		u, err := url.Parse(channel.PlaybackUrl)
		if err != nil {
			return nil, err
		}
		fmt.Println(u.RawQuery)

		q, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			return nil, err
		}

		jwtString := q.Get("token")

		token, _, err := new(jwt.Parser).ParseUnverified(jwtString, jwt.MapClaims{})
		if err != nil {
			return nil, err
		}

		tokenExpiration, err := token.Claims.GetExpirationTime()
		if err != nil {
			return nil, err
		}

		channel.Token = *&KickToken{
			Token:   jwtString,
			Expires: tokenExpiration.Unix(),
		}

		return &channel, nil
	}
}

func KickCheckIfLive(kickChannelObject *KickChannelStruct, channel string) (string, bool) {
	log.Printf("[Kick] [%s] Checking if live", channel)

	m3u8, err := getKickLiveM3u8(channel, kickChannelObject)
	if err != nil {
		log.Printf("[Kick] [%s] %v", channel, err)
		return "", false
	}

	return m3u8, true
}

func getKickLiveM3u8(channel string, kickChannelObject *KickChannelStruct) (string, error) {
	log.Printf("[Kick] [%s] Getting m3u8", channel)
	client := resty.New().R()

	resp, _ := client.
		Get(kickChannelObject.PlaybackUrl)

	if resp.StatusCode() != 200 {
		if resp.StatusCode() != 404 {
			log.Printf("Unexpected status code, expected %d, got %d instead", 200, resp.StatusCode())
		}
		return "", errors.New("is not live..")
	}

	p, listType, err := m3u8.DecodeFrom(bytes.NewReader(resp.Body()), true)
	if err != nil {
		return "", err
	}
	switch listType {
	case m3u8.MASTER:
		for _, variant := range p.(*m3u8.MasterPlaylist).Variants {
			if strings.EqualFold(variant.Video, "chunked") {
				return variant.URI, nil
			}
		}
	}
	return "", errors.New("Cannot find the chunked variant..")
}

func KickCheckIfUserExists(channel string) bool {
	log.Printf("[Kick] [%s] Checking if user exists", channel)
	client := resty.New()

	resp, _ := client.R().
		SetHeader("Accept", "application/json").
		Get(KICK_API_BASE + "/channels/" + channel)

	if resp.StatusCode() != 200 {
		log.Printf("Unexpected status code, expected %d, got %d instead", 200, resp.StatusCode())
		log.Printf(string(resp.Body()))
		return false
	}

	return true
}
