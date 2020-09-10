package main

import (
	"crypto/sha1"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
)

// helpers

func shorten(longURL string) string {
	hasher := sha1.New()

	hasher.Write([]byte(longURL))
	sum := hasher.Sum(nil)

	shortURL := fmt.Sprintf("%x", sum)[:6]

	return shortURL
}

// URLShortener URL shortener server data structure
type URLShortener struct {
	port int

	expanderRoute   string
	shortenRoute    string
	statisticsRoute string

	mux sync.Mutex

	shorts map[string]*urlInfo
}

type urlInfo struct {
	longURL string

	// statistics
	shortURL string
	count    int
}

func (cache *URLShortener) addURL(longURL string, shortURL string) {
	cache.mux.Lock()
	defer cache.mux.Unlock()

	cache.shorts[shortURL] = &urlInfo{longURL, shortURL, 0}
}

func (cache *URLShortener) getURL(shortURL string) (string, error) {
	cache.mux.Lock()
	defer cache.mux.Unlock()

	if info, ok := cache.shorts[shortURL]; ok {
		return info.longURL, nil
	}

	return "", fmt.Errorf("short URL not found: %s", shortURL)
}

func (cache *URLShortener) incrementURLCounter(shortURL string) {
	cache.mux.Lock()
	defer cache.mux.Unlock()

	cache.shorts[shortURL].count++
}

func (cache *URLShortener) getStatistics() string {
	cache.mux.Lock()
	defer cache.mux.Unlock()

	visits := 0
	stats := make([]string, len(cache.shorts))
	i := 0

	for shortURL, info := range cache.shorts {
		counter := info.count
		longURL := info.longURL

		visits += counter
		stats[i] = fmt.Sprintf("URL: [%s] %s visited %v time(s)", shortURL, longURL, counter)

		i++
	}

	statistics := fmt.Sprintf("Some statistics:\n\n%s\n\nTotal visits: %v", strings.Join(stats, "\n"), visits)

	return statistics
}

// handlers

func (cache *URLShortener) shortenHandler(w http.ResponseWriter, r *http.Request) {
	longURL := r.URL.Path[len(cache.shortenRoute):]
	shortURL := shorten(longURL)

	cache.addURL(longURL, shortURL)

	linkAddress := fmt.Sprintf("http://localhost:%v", cache.port)
	hrefAddress := fmt.Sprintf("%s/%s", linkAddress, shortURL)
	hrefText := fmt.Sprintf("%s -> %s", shortURL, longURL)

	fmt.Fprintf(w, "<a href=\"%s\">%s</a>", hrefAddress, hrefText)
}

func (cache *URLShortener) statisticsHandler(w http.ResponseWriter, r *http.Request) {
	stats := cache.getStatistics()
	fmt.Fprintf(w, "%s", stats)
}

func (cache *URLShortener) expanderHandler(w http.ResponseWriter, r *http.Request) {
	shortURLCandidate := r.URL.Path[len(cache.expanderRoute):]

	redirectURL, err := cache.getURL(shortURLCandidate)

	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	cache.incrementURLCounter(shortURLCandidate)

	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

func main() {
	cache := URLShortener{
		port: 9090,

		expanderRoute:   "/",
		shortenRoute:    "/shorten/",
		statisticsRoute: "/statistics",

		shorts: make(map[string]*urlInfo),
	}

	http.HandleFunc(cache.shortenRoute, cache.shortenHandler)
	http.HandleFunc(cache.statisticsRoute, cache.statisticsHandler)
	http.HandleFunc(cache.expanderRoute, cache.expanderHandler)

	listenAddress := fmt.Sprintf(":%v", cache.port)

	log.Fatal(http.ListenAndServe(listenAddress, nil))
}
