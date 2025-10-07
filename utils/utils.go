package utils

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func FormatURLs(urlFile string, concurrency int) error {
	f, err := os.Open(urlFile)
	if err != nil {
		return fmt.Errorf("open urls file: %w", err)
	}
	defer f.Close()

	urls := []string{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		if line != "" {
			urls = append(urls, line)
		}
	}
	if err := s.Err(); err != nil {
		return fmt.Errorf("scan urls file: %w", err)
	}

	baseDir := filepath.Dir(urlFile)
	logins, err := readLines(filepath.Join(baseDir, "login.txt"))
	if err != nil {
		return fmt.Errorf("read login.txt: %w", err)
	}
	passes, err := readLines(filepath.Join(baseDir, "pass.txt"))
	if err != nil {
		return fmt.Errorf("read pass.txt: %w", err)
	}

	outPath := filepath.Join(baseDir, "combinations.txt")
	outFile, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer outFile.Close()
	tasks := make(chan string)
	results := make(chan string)
	done := make(chan struct{})

	go func() {
		w := bufio.NewWriter(outFile)
		for r := range results {
			w.WriteString(r)
		}
		w.Flush()
		close(done)
	}()

	if concurrency <= 0 {
		concurrency = runtime.NumCPU()
	}
	for i := 0; i < concurrency; i++ {
		go func() {
			for t := range tasks {
				results <- t
			}
		}()
	}

	for _, url := range urls {
		for _, login := range logins {
			for _, pass := range passes {
				line := fmt.Sprintf("%s:%s:%s\n", url, login, pass)
				tasks <- line
			}
		}
	}

	close(tasks)
	close(results)
	<-done

	return nil
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	res := []string{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		if line != "" {
			res = append(res, line)
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return res, nil
}

