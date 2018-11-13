package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/deanishe/awgo"
)

const star rune = '⭑'

var client = &http.Client{
	Timeout: time.Second * 5,
}

func debug(format string, a ...interface{}) {
	if os.Getenv("DEBUG") != "" {
		fmt.Fprintf(os.Stderr, format+"\n", a...)
	}
}

func md5hash(s string) string {
	h := md5.New()
	io.WriteString(h, s)
	sum := h.Sum(nil)
	return hex.EncodeToString(sum)
}

// false -> new file
// true  -> already exists
func openFileIfNotExists(filename string) (*os.File, bool, error) {
	_, err := os.Stat(filename)
	if os.IsNotExist(err) {
		f, err := os.Create(filename)
		return f, false, err
	}
	if err != nil {
		return nil, false, err
	}
	return nil, true, err
}

func downloadAllImages(ctx context.Context, concurrency int, urls []string) []*aw.Icon {
	die := func(format string, a ...interface{}) {
		fmt.Fprintf(os.Stderr, "error: "+format+"\n", a...)
		// yes, deferred function calls will run even if Goexit() is called
		// (https://play.golang.org/p/LZ5Mt6F1DQW) DONT CALL IN MAIN GO ROUTINE
		runtime.Goexit()
	}
	output := make([]*aw.Icon, len(urls))
	var wg sync.WaitGroup
	sem := make(chan bool, concurrency)
	dl := func(i int, url string) {
		output[i] = aw.IconError
		filename := fmt.Sprintf(
			"%s/net.nkcmr.alfred-apple-app-search/%s.png",
			strings.TrimRight(os.TempDir(), "/"),
			md5hash(url),
		)
		if err := os.MkdirAll(filepath.Dir(filename), os.ModePerm); err != nil {
			die(err.Error())
			return
		}
		f, x, err := openFileIfNotExists(filename)
		if err != nil {
			die(
				"failed to create or open file for downloaded artwork: %s",
				err.Error(),
			)
			return
		}
		defer func() {
			if f != nil {
				defer f.Close()
			}
		}()
		if !x {
			debug("downloading: %s to %s", url, filename)
			resp, err := client.Get(url)
			if err != nil {
				die("failed to request artwork: %s", err.Error())
				return
			}
			defer resp.Body.Close()
			if _, err := io.Copy(f, resp.Body); err != nil {
				die("failed to download artwork: %s", err.Error())
				return
			}
		} else {
			debug("file is cached (%s)", filename)
		}
		output[i] = &aw.Icon{
			Type:  aw.IconTypeImage,
			Value: filename,
		}
	}
	for i, u := range urls {
		wg.Add(1)
		go func(i int, u string) {
			defer func() {
				<-sem
				wg.Done()
			}()
			sem <- true
			dl(i, u)
		}(i, u)
	}
	wg.Wait()
	return output
}

func sigContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		defer cancel()
		c := make(chan os.Signal)
		signal.Notify(c, syscall.SIGTERM, syscall.SIGINT)
		fmt.Printf("signal: %s\n", <-c)
	}()
	return ctx
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("fatal error: %+v", r)
			os.Exit(1)
		}
	}()
	ctx := sigContext()
	url, err := url.ParseRequestURI(
		"https://itunes.apple.com/search?media=software&entity=macSoftware&limit=20",
	)
	if err != nil {
		panic(err)
	}
	q := url.Query()
	q.Set("term", os.Args[1])
	url.RawQuery = q.Encode()
	req, err := http.NewRequest("GET", url.String(), http.NoBody)
	if err != nil {
		panic(err)
	}
	debug("sending request: %s %s", req.Method, req.URL.String())
	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		panic(fmt.Errorf("non-ok status code returned (%d)", resp.StatusCode))
	}
	var results struct {
		Results []struct {
			ID         int64   `json:"trackId"`
			Name       string  `json:"trackName"`
			Artwork    string  `json:"artworkUrl512"`
			URL        string  `json:"trackViewUrl"`
			Rating     float64 `json:"averageUserRating"`
			PriceFmt   string  `json:"formattedPrice"`
			NumRatings int     `json:"userRatingCount"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		panic(err)
	}
	debug("successfully downloaded results (%d results)", len(results.Results))
	images := make([]string, len(results.Results))
	fb := aw.NewFeedback()
	for i, res := range results.Results {
		item := new(aw.Item).
			Title(res.Name).
			Subtitle(
				fmt.Sprintf(
					"%s | %s(%d ratings)",
					res.PriceFmt,
					func() string {
						if res.Rating == float64(0) {
							return ""
						}
						return strings.Repeat(string(star), int(res.Rating)) + " "
					}(),
					res.NumRatings,
				),
			).
			Arg(fmt.Sprintf("macappstores://itunes.apple.com/app/id%d", res.ID)).
			Valid(true).
			IsFile(false)
		item.NewModifier(aw.ModAlt).Arg(res.URL).Valid(true).Subtitle("Open in browser")
		fb.Items = append(fb.Items, item)
		images[i] = res.Artwork
	}
	icons := downloadAllImages(ctx, runtime.NumCPU(), images)
	for i := range icons {
		fb.Items[i] = fb.Items[i].Icon(icons[i])
	}
	json.NewEncoder(os.Stdout).Encode(fb)
}
