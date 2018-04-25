package actions

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"

	"github.com/gobuffalo/pop"

	"github.com/mikkyang/id3-go"

	egdp "github.com/Azure/azure-sdk-for-go/services/eventgrid/2018-01-01/eventgrid"
	"github.com/Azure/azure-storage-blob-go/2016-05-31/azblob"
	"github.com/Azure/buffalo-azure/sdk/eventgrid"
	"github.com/gobuffalo/buffalo"
	"github.com/marstr/musicvotes/models"
	"github.com/pkg/errors"
)

func IngressBlobCreated(c buffalo.Context, e eventgrid.Event, payload egdp.StorageBlobCreatedEventData) error {
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

	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return errors.WithStack(errors.New("no transaction found"))
	}

	verrs, err := tx.ValidateAndCreate(song)
	if err != nil {
		return errors.WithStack(err)
	}

	if verrs.HasAny() {
		return c.Error(http.StatusBadRequest, errors.WithStack(errors.New("song is invalid")))
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
