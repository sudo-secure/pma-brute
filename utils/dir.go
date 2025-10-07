package utils

import (
	"bufio"
	"crypto/tls"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	keywords = []string{
		"phpMyAdmin",
	}

	openKeywords = []string{
		"pma_navigation",
	}

	userAgents = []string{
		"Mozilla/5.0 (Linux; Android 10; ZenFone Max Pro M1 Build/QQ3A.200605.002; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/74.0.3729.186 Mobile Safari/537.36",
	}

	outputMutex sync.Mutex
	wg          sync.WaitGroup
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func randomUserAgent() string {
	return userAgents[rand.Intn(len(userAgents))]
}

func check(rawURL string) {
	defer wg.Done()

	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return
	}

	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		log.Printf("[-] Invalid URL format: %s", rawURL)
		return
	}

	fullURL := strings.TrimRight(rawURL, "/") + "/phpmyadmin/"

	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return http.ErrUseLastResponse
			}
			return nil
		},
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		log.Printf("[!] Error creating request: %v", err)
		return
	}
	req.Header.Set("User-Agent", randomUserAgent())

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[-] Error response %s: %v", fullURL, err)
		return
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[-] Error reading response: %v", err)
		return
	}

	body := string(bodyBytes)

	for _, kw := range keywords {
		if strings.Contains(body, kw) {
			log.Printf("[FOUND phpMyAdmin panel] %s", fullURL)
			writeResult(fullURL, "phpmyadmin.txt")
			return
		}
	}

	for _, kw := range openKeywords {
		if strings.Contains(body, kw) {
			log.Printf("[FOUND OPEN phpMyAdmin panel] %s", fullURL)
			writeResult(fullURL, "phpmyadmin_open.txt")
			return
		}
	}

	log.Printf("[-] Not found: %s", fullURL)
}

func writeResult(url, filename string) {
	outputMutex.Lock()
	defer outputMutex.Unlock()

	file, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("[!] Error opening file %s: %v", filename, err)
		return
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	writer.WriteString(url + "\n")
	writer.Flush()
}

func worker(domains <-chan string) {
	for domain := range domains {
		check(domain)
	}
}

func CheckURLs(inputFile string, workers int) {
	file, err := os.Open(inputFile)
	if err != nil {
		log.Fatalf("Error opening file: %v", err)
	}
	defer file.Close()

	domainChan := make(chan string, 100)

	for i := 0; i < workers; i++ {
		go worker(domainChan)
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		domain := scanner.Text()
		wg.Add(1)
		domainChan <- domain
	}

	close(domainChan)
	wg.Wait()
	log.Println("[✓] URL check finished.")
}
