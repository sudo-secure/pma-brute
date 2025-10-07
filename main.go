package main

import (
	"pwa-bc/utils"
	"pwa-bc/checker"
)

func main() {
	utils.CheckURLs("urls.txt", 15)
	utils.FormatURLs("phpmyadmin.txt", 15)
	checker.RunChecker("combinations.txt", 30)
}
