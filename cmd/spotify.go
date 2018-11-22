package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/zmb3/spotify"
	"golang.org/x/net/context"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

// Spotify gets a list of recently played tracks
func Spotify() error {
	// Creates a bq client.
	ctx := context.Background()
	projectID := os.Getenv("GOOGLE_PROJECT")
	datasetName := os.Getenv("GOOGLE_DATASET")
	tableName := os.Getenv("GOOGLE_TABLE")
	accountJSON := os.Getenv("GOOGLE_JSON")

	creds, err := google.CredentialsFromJSON(ctx, []byte(accountJSON), bigquery.Scope)
	if err != nil {
		return fmt.Errorf("Failed to get creds from json: %v", err)
	}
	bigqueryClient, err := bigquery.NewClient(ctx, projectID, option.WithCredentials(creds))
	if err != nil {
		return fmt.Errorf("Failed to create client: %v", err)
	}
	// loads in the table schema from file
	jsonSchema, err := ioutil.ReadFile("schema.json")
	if err != nil {
		return fmt.Errorf("Failed to create schema: %v", err)
	}
	schema, err := bigquery.SchemaFromJSON(jsonSchema)
	if err != nil {
		return fmt.Errorf("Failed to parse schema: %v", err)
	}
	u := bigqueryClient.Dataset(datasetName).Table(tableName).Uploader()
	mostRecentTimestamp, err := mostRecentTimestamp(ctx, bigqueryClient, projectID, datasetName, tableName)
	if err != nil {
		return fmt.Errorf("Failed to get most recent timestamp: %v", err)
	}

	// Creates a spotify client
	spotifyClient := buildClient()
	recentlyPlayed, err := spotifyClient.PlayerRecentlyPlayedOpt(&spotify.RecentlyPlayedOptions{Limit: 50})
	if err != nil {
		return fmt.Errorf("Failed to get recent plays: %v", err)
	}
	for _, item := range recentlyPlayed {
		if mostRecentTimestamp.Unix() < item.PlayedAt.Unix() {
			fullTrack, err := spotifyClient.GetTrack(item.Track.ID)
			if err != nil {
				return fmt.Errorf("Failed to get full track: %v", err)
			}

			var artists []string
			for _, a := range item.Track.Artists {
				artists = append(artists, a.Name)
			}
			var image string
			if len(fullTrack.Album.Images) > 0 {
				image = fullTrack.Album.Images[0].URL
			}
			// creates items to be saved in big query
			var vss []*bigquery.ValuesSaver
			vss = append(vss, &bigquery.ValuesSaver{
				Schema:   schema,
				InsertID: fmt.Sprintf("%v", item.PlayedAt.Unix()),
				Row: []bigquery.Value{
					item.Track.Name,
					strings.Join(artists, ", "),
					fullTrack.Album.Name,
					fmt.Sprintf("%d", item.PlayedAt.Unix()),
					bigquery.NullInt64{Int64: int64(item.Track.Duration), Valid: true},
					fmt.Sprintf("%s", item.Track.ID),
					image,
					fmt.Sprintf("%d", time.Now().Unix()),
					"spotify",
					"", // youtube_id
					"", // youtube_category_id
					"", // soundcloud_id
					"", // soundcloud_permalink
					"", // shazam_id
					"", // shazam_permalink
				},
			})

			// upload the items
			err = u.Put(ctx, vss)
			if err != nil {
				if pmErr, ok := err.(bigquery.PutMultiError); ok {
					for _, rowInsertionError := range pmErr {
						log.Println(rowInsertionError.Errors)
					}
				}

				return fmt.Errorf("Failed to insert row: %v", err)
			}
			fmt.Printf("%v %s\n", item.PlayedAt, item.Track.Name)
		}
	}

	return nil
}

func mostRecentTimestamp(ctx context.Context, client *bigquery.Client, projectID string, datasetName string, tableName string) (time.Time, error) {
	var t time.Time
	queryString := fmt.Sprintf(
		"SELECT timestamp FROM `%s.%s.%s` WHERE source = \"spotify\" OR source IS NULL ORDER BY timestamp DESC LIMIT 1",
		projectID,
		datasetName,
		tableName,
	)
	q := client.Query(queryString)
	it, err := q.Read(ctx)
	if err != nil {
		return t, fmt.Errorf("Failed query for recent timestamp: %v", err)
	}
	var l struct {
		Timestamp time.Time
	}
	for {
		err := it.Next(&l)
		if err == iterator.Done {
			break
		}
		if err != nil {
			fmt.Println(err)
			return t, fmt.Errorf("Failed reading results for time: %v", err)
		}
		break
	}

	return l.Timestamp, nil
}
