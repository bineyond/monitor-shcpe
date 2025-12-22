package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/PuerkitoBio/goquery"
)

func main() {
	url := "http://www.shcpe.com.cn/content/shcpe/news/announce.html"

	resp, err := http.Get(url)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Fatalf("status code error: %d %s", resp.StatusCode, resp.Status)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("页面标题:", doc.Find("title").Text())
	fmt.Println("\n--- 完整的页面结构分析 ---\n")

	// 分析你指定的区域
	fmt.Println("指定的选择器区域内容:")
	doc.Find("body > div.eui-page-container > main > div:nth-child(2) > div.eui-layout-content.eui-layout-multiple > div.substance").Each(func(i int, s *goquery.Selection) {
		html, _ := s.Html()
		fmt.Printf("区域 %d 的HTML结构:\n%s\n", i, html)
	})

	fmt.Println("\n--- 查看所有 li 元素 ---")
	doc.Find("li").Each(func(i int, s *goquery.Selection) {
		if i < 5 {
			fmt.Printf("Li %d: %s\n", i, s.Text())
		}
	})

	fmt.Println("\n--- 查看所有 a 链接 ---")
	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		if i < 10 {
			href, _ := s.Attr("href")
			fmt.Printf("Link %d: href=%s, text=%s\n", i, href, s.Text())
		}
	})

	fmt.Println("\n--- 按通用模式查找新闻列表 ---")
	doc.Find(".news-list li, ul li").Each(func(i int, s *goquery.Selection) {
		if i < 10 {
			fmt.Printf("News item %d: %s\n", i, s.Text())
			s.Find("a").Each(func(j int, a *goquery.Selection) {
				href, _ := a.Attr("href")
				fmt.Printf("  Link: %s -> %s\n", href, a.Text())
			})
		}
	})
}
