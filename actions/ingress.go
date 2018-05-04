package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/gobuffalo/pop"

	"github.com/mikkyang/id3-go"

	egdp "github.com/Azure/azure-sdk-for-go/services/eventgrid/2018-01-01/eventgrid"
	"github.com/Azure/azure-storage-blob-go/2016-05-31/azblob"
	"github.com/Azure/buffalo-azure/sdk/eventgrid"
	"github.com/gobuffalo/buffalo"
	"github.com/marstr/musicvotes/models"
	"github.com/pkg/errors"
)

type IngressSubscriber struct {
	eventgrid.Subscriber
	cache *eventgrid.Cache
}

func NewIngressSubscriber() *IngressSubscriber {
	dispatcher := eventgrid.NewTypeDispatchSubscriber(&eventgrid.BaseSubscriber{})

	created := &IngressSubscriber{
		Subscriber: dispatcher,
		cache:      &eventgrid.Cache{},
	}

	dispatcher.Bind("Microsoft.Storage.BlobCreated", created.BlobCreated)

	return created

}

func (s *IngressSubscriber) Show(c buffalo.Context) error {
	eventID := c.Param("event_id")
	var found bool
	for _, entry := range s.cache.List() {
		if strings.EqualFold(eventID, entry.ID) {
			if c.Logger() != nil {
				c.Logger().Debug("matching event found")
			}

			var formatted bytes.Buffer
			if err := json.Indent(&formatted, entry.Data, "", "\t"); err != nil {
				return c.Error(http.StatusBadRequest, err)
			}
			c.Data()["event"] = entry
			c.Data()["eventData"] = formatted.String()
			found = true
			break
		}
	}

	if !found {
		return c.Error(http.StatusNotFound, errors.New("event not found"))
	}

	return c.Render(http.StatusOK, r.HTML("ingress/show"))
}

func (s *IngressSubscriber) List(c buffalo.Context) error {
	c.Data()["events"] = s.cache.List()
	return c.Render(http.StatusOK, r.HTML("ingress/index"))
}

// BlobCreated implements some behavior that should be impleen
func (s *IngressSubscriber) BlobCreated(c buffalo.Context, e eventgrid.Event) error {
	var payload egdp.StorageBlobCreatedEventData
	if err := json.Unmarshal(e.Data, &payload); err != nil {
		return c.Error(http.StatusBadRequest, err)
	}

	if payload.URL == nil {
		return c.Error(http.StatusBadRequest, errors.New("no blob URL was present"))
	}

	u, err := url.Parse(*payload.URL)
	if err != nil {
		return c.Error(http.StatusBadRequest, fmt.Errorf("%q is not a well formatted URL", *payload.URL))
	}

	handle, err := ioutil.TempFile("", "musicvotes_song_")
	if err != nil {
		return c.Error(http.StatusInternalServerError, errors.New("unable to save blob for ingestion"))
	}
	defer os.Remove(handle.Name())

	err = DownloadBlob(c, u, handle)
	if err != nil {
		return c.Error(http.StatusInternalServerError, fmt.Errorf("unable to download %q", u.String()))
	}
	handle.Close()

	songReader, err := id3.Open(handle.Name())
	if err != nil {
		return c.Error(http.StatusInternalServerError, errors.WithStack(fmt.Errorf("unable to parse %q as an audio file", u.String())))
	}
	defer songReader.Close()

	song := &models.Song{
		Title:  songReader.Title(),
		Artist: songReader.Artist(),
		Vote:   1,
		Url:    u.String(),
	}

	if song.Title == "" {
		song.Title = "Unknown Title"
	}

	if song.Artist == "" {
		song.Artist = "Unknown Artist"
	}

	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return c.Error(http.StatusInternalServerError, errors.New("no transaction found"))
	}

	verrs, err := tx.ValidateAndCreate(song)
	if err != nil {
		return c.Error(http.StatusInternalServerError, err)
	}

	if verrs.HasAny() {
		return c.Error(http.StatusBadRequest, fmt.Errorf("song is invalid: %s", verrs.String()))
	}

	return c.Render(http.StatusCreated, r.Auto(c, song))
}

// DownloadBlob fetches a copy of a blob for processing.
func DownloadBlob(ctx context.Context, source *url.URL, destination *os.File) error {
	pipeline := azblob.NewPipeline(azblob.NewAnonymousCredential(), azblob.PipelineOptions{})
	blobURL := azblob.NewBlobURL(*source, pipeline)
	rs := azblob.NewDownloadStream(ctx, blobURL.GetBlob, azblob.DownloadStreamOptions{})
	_, err := io.Copy(destination, rs)
	return err
}
