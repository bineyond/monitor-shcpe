package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/chromedp/chromedp"
)

func main() {
	// Sample detail URL
	url := "http://www.shcpe.com.cn/content/shcpe/news/announce.html?articleType=news-announce&articleId=WZ202511211991802745492852736"

	opts := []chromedp.ExecAllocatorOption{
		chromedp.NoFirstRun,
		chromedp.Headless,
		chromedp.NoDefaultBrowserCheck,
		chromedp.NoSandbox,
		chromedp.DisableGPU,
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-default-apps", true),
	}

	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	var htmlContent string

	fmt.Printf("Fetching %s...\n", url)
	err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitVisible(".dynamic-content", chromedp.ByQuery),
		chromedp.Sleep(2*time.Second),
		chromedp.OuterHTML("html", &htmlContent),
	)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	os.WriteFile("debug_detail.html", []byte(htmlContent), 0644)
	fmt.Println("Saved to debug_detail.html")
}
