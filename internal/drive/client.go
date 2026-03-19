package drive

import (
	"context"
	"os"

	"golang.org/x/oauth2"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

type Client struct {
	service     *drive.Service
	tokenSource oauth2.TokenSource
}

func NewClient(ctx context.Context, config *oauth2.Config, token *oauth2.Token) (*Client, error) {
	client := oauth2.NewClient(ctx, config.TokenSource(ctx, token))
	service, err := drive.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, err
	}

	return &Client{
		service:     service,
		tokenSource: config.TokenSource(ctx, token),
	}, nil
}

func (c *Client) UploadFile(ctx context.Context, filePath, name string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	driveFile := &drive.File{
		Name:     name,
		MimeType: "video/mp4",
	}

	_, err = c.service.Files.Create(driveFile).
		Media(file).
		Context(ctx).
		Do()

	return err
}
