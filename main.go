package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/chromedp"
	_ "modernc.org/sqlite"
)

type Config struct {
	Targets             []TargetConfig `json:"targets"`
	WebhookURL          string         `json:"webhook_url"`
	CheckInterval       int            `json:"check_interval"`
	DBFile              string         `json:"db_file"`
	DeepSeekAPIKey      string         `json:"deepseek_api_key"`
	IgnoreOlderThanDays int            `json:"ignore_older_than_days"`
}

type TargetConfig struct {
	ID       int64    `json:"id"`
	Name     string   `json:"name"`
	URL      string   `json:"url"`
	Keywords []string `json:"keywords"`
	Enabled  bool     `json:"enabled"`
}

// ... (Rest of the file)

type WebhookMessage struct {
	MsgType  string           `json:"msgtype"`
	Markdown *MarkdownContent `json:"markdown,omitempty"`
}

type MarkdownContent struct {
	Content string `json:"content"`
}

type Announcement struct {
	Title   string
	Date    string
	Link    string
	Content string
	Source  string // Added source name
}

func loadConfig() *Config {
	// Default config
	config := &Config{
		CheckInterval: 7200,
		DBFile:        "monitor.db",
		Targets:       []TargetConfig{
			// {
			// 	Name:    "票交所公告",
			// 	URL:     "http://www.shcpe.com.cn/content/shcpe/news/announce.html", // Assumption keeping existing
			// 	Enabled: true,
			// },
			// {
			// 	Name:    "票据信息披露平台-通知公告",
			// 	URL:     "https://disclosure.cpisp.shcpe.com.cn/#/notice/noticeTicket/news-events",
			// 	Enabled: true,
			// },
		},
		IgnoreOlderThanDays: 7,
	}

	// Try to load from config.json
	file, err := os.Open("config.json")
	if err == nil {
		defer file.Close()
		decoder := json.NewDecoder(file)
		if err := decoder.Decode(config); err != nil {
			fmt.Printf("Warning: Failed to parse config.json: %v\nUsing defaults/env vars.\n", err)
		}
	} else {
		fmt.Println("Note: config.json not found, using default configuration.")
	}

	// Env var overrides
	if webhookURL := os.Getenv("WEBHOOK_URL"); webhookURL != "" {
		config.WebhookURL = webhookURL
	}
	if dbFile := os.Getenv("DB_FILE"); dbFile != "" {
		config.DBFile = dbFile
	}

	return config
}

func sendWeChatWebhook(message string, webhookURL string) error {
	msg := WebhookMessage{
		MsgType: "markdown",
		Markdown: &MarkdownContent{
			Content: message,
		},
	}

	jsonData, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %v", err)
	}

	resp, err := http.Post(webhookURL, "application/json", strings.NewReader(string(jsonData)))
	if err != nil {
		return fmt.Errorf("failed to send webhook: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// Database helper functions

func initDB(dbFile string) (*sql.DB, error) {
	// Enable WAL mode and set busy timeout to reduce locking errors
	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", dbFile)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	createAnnouncementsTable := `CREATE TABLE IF NOT EXISTS announcements (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		url TEXT NOT NULL UNIQUE,
		title TEXT,
		publish_date TEXT,
		source TEXT,
		fetched_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);`

	if _, err = db.Exec(createAnnouncementsTable); err != nil {
		return nil, fmt.Errorf("failed to create announcements table: %v", err)
	}

	createTargetsTable := `CREATE TABLE IF NOT EXISTS targets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		url TEXT NOT NULL UNIQUE,
		keywords TEXT,
		enabled BOOLEAN DEFAULT 1,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);`

	if _, err = db.Exec(createTargetsTable); err != nil {
		return nil, fmt.Errorf("failed to create targets table: %v", err)
	}

	return db, nil
}

func syncTargets(db *sql.DB, config *Config) {
	fmt.Println("Syncing targets from config.json to database...")
	tx, err := db.Begin()
	if err != nil {
		fmt.Printf("Sync error: %v\n", err)
		return
	}

	// SQLite UPSERT syntax: INSERT INTO ... ON CONFLICT(url) DO UPDATE SET ...
	stmt, err := tx.Prepare(`
		INSERT INTO targets (name, url, keywords, enabled) 
		VALUES (?, ?, ?, ?)
		ON CONFLICT(url) DO UPDATE SET
		name=excluded.name,
		keywords=excluded.keywords,
		enabled=excluded.enabled
	`)
	if err != nil {
		fmt.Printf("Sync prepare error: %v\n", err)
		return
	}
	defer stmt.Close()

	count := 0
	for _, t := range config.Targets {
		keywordsStr := strings.Join(t.Keywords, ",")

		// Map bool to int for SQLite if needed, but modernc/sqlite driver usually handles bool.
		// However, to be explicit and safe:
		enabled := t.Enabled

		_, err = stmt.Exec(t.Name, t.URL, keywordsStr, enabled)
		if err != nil {
			fmt.Printf("Failed to sync target %s: %v\n", t.Name, err)
		} else {
			count++
		}
	}

	if err := tx.Commit(); err != nil {
		fmt.Printf("Sync commit error: %v\n", err)
		return
	}
	fmt.Printf("Synced %d targets to database.\n", count)
}

func migrateHistory(db *sql.DB, historyFile string) {
	data, err := os.ReadFile(historyFile)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		fmt.Printf("Warning: Failed to read legacy history file: %v\n", err)
		return
	}

	fmt.Println("Migrating legacy history to database...")
	lines := strings.Split(string(data), "\n")
	tx, err := db.Begin()
	if err != nil {
		fmt.Printf("Migration error: %v\n", err)
		return
	}

	stmt, err := tx.Prepare("INSERT OR IGNORE INTO announcements (url, fetched_at) VALUES (?, ?)")
	if err != nil {
		fmt.Printf("Migration prepare error: %v\n", err)
		return
	}
	defer stmt.Close()

	count := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			_, err = stmt.Exec(line, time.Now())
			if err == nil {
				count++
			}
		}
	}

	if err := tx.Commit(); err != nil {
		fmt.Printf("Migration commit error: %v\n", err)
		return
	}

	fmt.Printf("Migrated %d URLs to database.\n", count)

	// Rename old file
	if err := os.Rename(historyFile, historyFile+".bak"); err != nil {
		fmt.Printf("Warning: Failed to rename legacy history file: %v\n", err)
	} else {
		fmt.Println("Legacy history file renamed to " + historyFile + ".bak")
	}
}

func isURLVisited(db *sql.DB, url string) bool {
	var exists int
	err := db.QueryRow("SELECT 1 FROM announcements WHERE url = ?", url).Scan(&exists)
	return err == nil
}

func saveAnnouncement(db *sql.DB, ann Announcement) error {
	var err error
	// Retry up to 5 times for database locking issues
	for i := 0; i < 5; i++ {
		_, err = db.Exec("INSERT INTO announcements (url, title, publish_date, source, fetched_at) VALUES (?, ?, ?, ?, ?)",
			ann.Link, ann.Title, ann.Date, ann.Source, time.Now())

		if err == nil {
			return nil
		}

		// If database is locked, wait and retry
		if strings.Contains(err.Error(), "database is locked") || strings.Contains(err.Error(), "SQLITE_BUSY") {
			time.Sleep(time.Duration(100*(i+1)) * time.Millisecond)
			continue
		}

		// For other errors (e.g. constraints), return immediately
		return err
	}
	return fmt.Errorf("failed after 5 retries: %v", err)
}

func fetchAnnouncementsWithChrome(url string, keywords []string) ([]Announcement, error) {
	// 创建chrome选项，减少错误信息
	opts := []chromedp.ExecAllocatorOption{
		chromedp.NoFirstRun,
		chromedp.Headless,
		chromedp.NoDefaultBrowserCheck,
		chromedp.NoSandbox,
		chromedp.DisableGPU,
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-default-apps", true),
		chromedp.Flag("disable-background-timer-throttling", true),
		chromedp.Flag("disable-backgrounding-occluded-windows", true),
		chromedp.Flag("disable-renderer-backgrounding", true),
		chromedp.Flag("disable-setuid-sandbox", true),
		chromedp.Flag("password-store", "basic"),
		chromedp.Flag("use-mock-keychain", true),
		chromedp.Flag("metrics-recording-only", true),
		chromedp.Flag("enable-automation", true),
		// 添加User-Agent，防止被反爬
		chromedp.UserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
	}

	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()

	ctx, cancel := chromedp.NewContext(allocCtx, chromedp.WithLogf(func(s string, args ...interface{}) {
		msg := fmt.Sprintf(s, args...)
		if !strings.Contains(msg, "unknown IPAddressSpace value") {
			fmt.Printf(s, args...)
		}
	}))
	defer cancel()

	// 设置超时
	ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	var htmlContent string

	fmt.Printf("Fetching URL: %s\n", url)
	err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.Sleep(5*time.Second),
		chromedp.OuterHTML("html", &htmlContent),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get page content: %v", err)
	}

	fmt.Printf("页面内容长度: %d\n", len(htmlContent))

	// 将HTML保存到文件以便调试 (使用URL的一部分作为文件名，避免覆盖)
	safeName := regexp.MustCompile(`[^a-zA-Z0-9]`).ReplaceAllString(url, "_")
	if len(safeName) > 50 {
		safeName = safeName[len(safeName)-50:]
	}
	os.WriteFile(fmt.Sprintf("debug_page_%s.html", safeName), []byte(htmlContent), 0644)

	// 使用goquery解析HTML
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %v", err)
	}

	var announcements []Announcement

	// Special handling for disclosure.cpisp.shcpe.com.cn
	if strings.Contains(url, "disclosure.cpisp.shcpe.com.cn") {
		doc.Find(".law-item").Each(func(i int, s *goquery.Selection) {
			title := strings.TrimSpace(s.Find(".law-item-title span").Text())
			date := strings.TrimSpace(s.Find(".publish-date").Text())

			// Build synthetic link since there is no direct detail page
			// Using query param to make it unique per title
			link := fmt.Sprintf("%s?title=%s", url, title)

			shouldInclude := false
			if len(keywords) == 0 {
				shouldInclude = true
			} else {
				for _, kw := range keywords {
					if strings.Contains(title, kw) {
						shouldInclude = true
						break
					}
				}
			}

			if shouldInclude && title != "" {
				announcements = append(announcements, Announcement{
					Title: title,
					Date:  date,
					Link:  link,
				})
			}
		})

		fmt.Printf("找到 %d 条公告 (disclosure)\n", len(announcements))
		return announcements, nil
	}

	// 查找所有包含articleId的链接
	doc.Find("a[targetid]").Each(func(i int, s *goquery.Selection) {
		href, hrefExists := s.Attr("href")
		if !hrefExists {
			return
		}

		// 查找标题
		title := ""
		titleElement := s.Find(".information")
		if titleElement.Length() > 0 {
			title = strings.TrimSpace(titleElement.Text())
		} else {
			// 备选方法，查找所有文本
			title = strings.TrimSpace(s.Text())
		}

		// 查找日期
		date := ""
		dayElement := s.Find(".day")
		yearElement := s.Find(".year")
		if dayElement.Length() > 0 && yearElement.Length() > 0 {
			day := strings.TrimSpace(dayElement.Text())
			year := strings.TrimSpace(yearElement.Text())
			date = fmt.Sprintf("%s.%s", year, day)
		}

		// 过滤逻辑
		shouldInclude := false
		if len(keywords) == 0 {
			// 如果没有关键字，默认包含所有（或者根据需要调整）
			// 这里假设如果没有关键字，就包含所有找到的条目
			shouldInclude = true
		} else {
			for _, kw := range keywords {
				if strings.Contains(title, kw) {
					shouldInclude = true
					break
				}
			}
		}

		if shouldInclude {
			if !strings.HasPrefix(href, "http") {
				href = "http://www.shcpe.com.cn" + href
			}

			announcements = append(announcements, Announcement{
				Title: title,
				Date:  date,
				Link:  href,
			})
		}
	})

	// 如果goquery方法没找到，尝试正则表达式（作为备选）
	if len(announcements) == 0 {
		fmt.Println("尝试使用正则表达式搜索...")

		// 稍微放宽正则以适应不同页面
		re := regexp.MustCompile(`href="([^"]+)"[^>]*targetid="WZ2025[^"]+"[^>]*>\s*<div[^>]+>\s*<div[^>]+>\s*<div[^>]+>(\d+)</div>\s*<div[^>]+>([^<]+)</div>\s*</div>\s*</div>\s*<div[^>]+>\s*<div[^>]+>\s*<div[^>]+></div>\s*<span[^>]+>([^<]+)</span>`)
		matches := re.FindAllStringSubmatch(htmlContent, -1)

		for _, match := range matches {
			if len(match) >= 5 {
				href := match[1]
				day := match[2]
				year := match[3]
				title := match[4]

				if !strings.HasPrefix(href, "http") {
					href = "http://www.shcpe.com.cn" + href
				}

				shouldInclude := false
				if len(keywords) == 0 {
					shouldInclude = true
				} else {
					for _, kw := range keywords {
						if strings.Contains(title, kw) {
							shouldInclude = true
							break
						}
					}
				}

				if shouldInclude {
					announcements = append(announcements, Announcement{
						Title: strings.TrimSpace(title),
						Date:  fmt.Sprintf("%s.%s", year, day),
						Link:  href,
					})
				}
			}
		}
	}

	fmt.Printf("找到 %d 条公告\n", len(announcements))

	return announcements, nil
}

func isTargetEnabled(db *sql.DB, id int64) bool {
	var enabled bool
	err := db.QueryRow("SELECT enabled FROM targets WHERE id = ?", id).Scan(&enabled)
	if err != nil {
		return false // Assume disabled on error or not found
	}
	return enabled
}

// getEnabledTargets fetches all enabled targets from the database.
func getEnabledTargets(db *sql.DB) ([]TargetConfig, error) {
	rows, err := db.Query("SELECT id, name, url, keywords, enabled FROM targets WHERE enabled = 1")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var targets []TargetConfig
	for rows.Next() {
		var t TargetConfig
		var keywordsStr string
		if err := rows.Scan(&t.ID, &t.Name, &t.URL, &keywordsStr, &t.Enabled); err != nil {
			// Log the error but continue processing other rows
			fmt.Printf("Error scanning target row: %v\n", err)
			continue
		}
		if keywordsStr != "" {
			t.Keywords = strings.Split(keywordsStr, ",")
		} else {
			t.Keywords = []string{}
		}
		targets = append(targets, t)
	}
	return targets, nil
}

func parseDate(dateStr string) (time.Time, error) {
	dateStr = strings.TrimSpace(dateStr)
	formats := []string{
		"2006.01.02",
		"2006-01-02",
		"2006/01/02",
		"2006年01月02日",
	}
	for _, format := range formats {
		if t, err := time.Parse(format, dateStr); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unknown date format")
}

func checkForNewAnnouncements(config *Config, db *sql.DB) error {
	newCount := 0

	// Fetch enabled targets from DB
	targets, err := getEnabledTargets(db)
	if err != nil {
		return fmt.Errorf("failed to fetch targets from db: %v", err)
	}

	for _, target := range targets {
		// Double check enabled status right before processing
		// This handles the case where user disables a target while the loop is running
		if !isTargetEnabled(db, target.ID) {
			fmt.Printf("Skipping disabled target: %s\n", target.Name)
			continue
		}

		fmt.Printf("\n正在检查: %s (%s)\n", target.Name, target.URL)
		announcements, err := fetchAnnouncementsWithChrome(target.URL, target.Keywords)
		if err != nil {
			fmt.Printf("Error fetching %s: %v\n", target.Name, err)
			continue
		}

		if len(announcements) == 0 {
			fmt.Printf("未在 %s 找到任何内容\n", target.Name)
			continue
		}

		for _, ann := range announcements {
			ann.Source = target.Name // Set source

			if !isURLVisited(db, ann.Link) {
				newCount++

				if err := saveAnnouncement(db, ann); err != nil {
					fmt.Printf("Failed to save announcement to DB: %v\n", err)
					// Verify we don't send notification if DB save fails, to avoid duplicates later
					continue
				}

				fmt.Printf("发现新内容 [%s]: %s\n", target.Name, ann.Title)

				message := fmt.Sprintf("### 新的%s\n\n**标题**: [%s](%s)\n\n**时间**: %s\n\n", target.Name, ann.Title, ann.Link, ann.Date)

				shouldNotify := true
				if config.IgnoreOlderThanDays > 0 && ann.Date != "" {
					parsedDate, err := parseDate(ann.Date)
					if err == nil {
						daysOld := int(time.Since(parsedDate).Hours() / 24)
						if daysOld > config.IgnoreOlderThanDays {
							shouldNotify = false
							fmt.Printf("跳过通知 [%s]: %s (已发布 %d 天，超过 %d 天限制)\n", target.Name, ann.Title, daysOld, config.IgnoreOlderThanDays)
						}
					} else {
						fmt.Printf("解析日期失败 [%s]: %v\n", ann.Date, err)
					}
				}

				if shouldNotify && config.WebhookURL != "" {
					if err := sendWeChatWebhook(message, config.WebhookURL); err != nil {
						fmt.Printf("Warning: Failed to send webhook: %v\n", err)
					} else {
						fmt.Printf("✓ 发送通知成功\n")
					}
				}

				time.Sleep(1 * time.Second)
			}
		}
	}

	if newCount > 0 {
		fmt.Printf("\n本轮检查完成，共发现 %d 条新内容\n", newCount)
	} else {
		fmt.Printf("\n本轮检查完成，暂无新内容\n")
	}

	return nil
}

// Monitor orchestrates the configuration and database
type Monitor struct {
	Config           *Config
	DB               *sql.DB
	Mu               sync.RWMutex
	ConfigUpdateChan chan bool
}

func (m *Monitor) saveGenericConfig() error {
	// Only save global config settings to file, targets are in DB
	// For backward compatibility or export, we could load targets from DB and put them in Config.
	// But let's keep Config lightweight for now.

	file, err := os.Create("config.json")
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	// We might want to empty Targets slice before saving to avoid confusion,
	// or we populate it from DB if we want config.json to be a full backup.
	// Let's populate it so config.json remains a valid backup.
	targets, _ := getAllTargets(m.DB)
	m.Config.Targets = targets

	return encoder.Encode(m.Config)
}

func getAllTargets(db *sql.DB) ([]TargetConfig, error) {
	rows, err := db.Query("SELECT id, name, url, keywords, enabled FROM targets")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var targets []TargetConfig
	for rows.Next() {
		var t TargetConfig
		var keywordsStr string
		if err := rows.Scan(&t.ID, &t.Name, &t.URL, &keywordsStr, &t.Enabled); err != nil {
			continue
		}
		if keywordsStr != "" {
			t.Keywords = strings.Split(keywordsStr, ",")
		} else {
			t.Keywords = []string{}
		}
		targets = append(targets, t)
	}
	return targets, nil
}

// API Handlers
func (m *Monitor) handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "GET" {
		m.Mu.RLock()
		defer m.Mu.RUnlock()

		// Populate targets from DB
		targets, err := getAllTargets(m.DB)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		m.Config.Targets = targets
		json.NewEncoder(w).Encode(m.Config)
		return
	}

	if r.Method == "POST" {
		var newConfig Config
		if err := json.NewDecoder(r.Body).Decode(&newConfig); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		m.Mu.Lock()
		// Update global settings
		m.Config.WebhookURL = newConfig.WebhookURL
		m.Config.CheckInterval = newConfig.CheckInterval
		m.Config.DeepSeekAPIKey = newConfig.DeepSeekAPIKey
		m.Config.IgnoreOlderThanDays = newConfig.IgnoreOlderThanDays

		// Update Targets in DB
		// Simple approach: Transactional delete all and re-insert, or careful diff.
		// For simplicity and to handle edits, let's process them.
		// However, frontend sends full list usually.
		err := m.updateTargetsInDB(newConfig.Targets)
		if err != nil {
			m.Mu.Unlock()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		m.saveGenericConfig()
		m.Mu.Unlock()

		// Signal config update
		// Non-blocking send in case channel is full (shouldn't happen with size 1, but safe)
		select {
		case m.ConfigUpdateChan <- true:
		default:
		}

		w.WriteHeader(http.StatusOK)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (m *Monitor) handleRepush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var titleNull, sourceNull, dateNull sql.NullString
	var title, link, date, source string

	err := m.DB.QueryRow("SELECT title, url, publish_date, source FROM announcements WHERE id = ?", req.ID).Scan(&titleNull, &link, &dateNull, &sourceNull)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Announcement not found", http.StatusNotFound)
		} else {
			http.Error(w, "Database error: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	title = titleNull.String
	date = dateNull.String
	source = sourceNull.String

	if title == "" {
		title = "无标题"
	}
	if date == "" {
		date = "未知日期"
	}
	if source == "" {
		source = "未知来源"
	}

	message := fmt.Sprintf("### [重推] 新的%s\n\n**标题**: [%s](%s)\n\n**时间**: %s\n\n", source, title, link, date)

	if m.Config.WebhookURL == "" {
		http.Error(w, "Webhook URL not configured", http.StatusBadRequest)
		return
	}

	if err := sendWeChatWebhook(message, m.Config.WebhookURL); err != nil {
		http.Error(w, "Failed to send webhook: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func (m *Monitor) updateTargetsInDB(targets []TargetConfig) error {
	tx, err := m.DB.Begin()
	if err != nil {
		return err
	}

	// For now, to ensure cleaner sync with basic frontend logic:
	// We can update existing by ID, insert new ones (ID=0), and delete missing ones.
	// Or simplistic replacement: Delete all, Insert all (loses history relation if we had any foreign keys, but we don't yet).
	// But we want to keep IDs stable if possible.

	// Better approach:
	// 1. Get existing IDs
	// 2. Map new config IDs
	// 3. Insert ID=0
	// 4. Update ID!=0
	// 5. Delete IDs not in new config

	// Since we are moving fast, let's try a robust loop.

	existingids := make(map[int64]bool)
	rows, err := m.DB.Query("SELECT id FROM targets")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id int64
			rows.Scan(&id)
			existingids[id] = true
		}
	}

	incomingids := make(map[int64]bool)

	stmtInsert, _ := tx.Prepare("INSERT INTO targets (name, url, keywords, enabled) VALUES (?, ?, ?, ?)")
	stmtUpdate, _ := tx.Prepare("UPDATE targets SET name=?, url=?, keywords=?, enabled=? WHERE id=?")
	defer stmtInsert.Close()
	defer stmtUpdate.Close()

	for _, t := range targets {
		keywordsStr := strings.Join(t.Keywords, ",")

		if t.ID > 0 && existingids[t.ID] {
			// Update
			_, err := stmtUpdate.Exec(t.Name, t.URL, keywordsStr, t.Enabled, t.ID)
			if err != nil {
				tx.Rollback()
				return err
			}
			incomingids[t.ID] = true
		} else {
			// Insert (New or lost ID)
			res, err := stmtInsert.Exec(t.Name, t.URL, keywordsStr, t.Enabled)
			if err != nil {
				tx.Rollback()
				return err
			}
			newId, _ := res.LastInsertId()
			incomingids[newId] = true
		}
	}

	// Delete missing
	for id := range existingids {
		if !incomingids[id] {
			_, err := tx.Exec("DELETE FROM targets WHERE id = ?", id)
			if err != nil {
				tx.Rollback()
				return err
			}
		}
	}

	return tx.Commit()
}

type HistoryItem struct {
	ID        int    `json:"id"`
	URL       string `json:"url"`
	Title     string `json:"title"`
	Source    string `json:"source"`
	FetchedAt string `json:"fetched_at"`
	Date      string `json:"date"`
}

type HistoryResponse struct {
	Items      []HistoryItem `json:"items"`
	Total      int           `json:"total"`
	Page       int           `json:"page"`
	TotalPages int           `json:"total_pages"`
}

func (m *Monitor) handleHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Parse pagination and filter params
	query := r.URL.Query()
	fmt.Printf("History Request: source=%s, sort=%s, order=%s\n", query.Get("source"), query.Get("sort_by"), query.Get("order"))

	page := 1
	limit := 20
	sortBy := "publish_date"
	order := "DESC"
	source := query.Get("source")

	if p := query.Get("page"); p != "" {
		fmt.Sscanf(p, "%d", &page)
	}
	if l := query.Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	if s := query.Get("sort_by"); s == "publish_date" || s == "fetched_at" {
		sortBy = s
	}
	if o := query.Get("order"); o == "ASC" || o == "DESC" {
		order = o
	}

	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit

	// Build WHERE clause
	whereClause := "1=1"
	args := []interface{}{}
	if source != "" && source != "all" {
		whereClause += " AND source = ?"
		args = append(args, source)
	}
	fmt.Printf("History Query: WHERE %s, Args: %v\n", whereClause, args)

	// Get total count
	var total int
	countQuery := "SELECT COUNT(*) FROM announcements WHERE " + whereClause
	err := m.DB.QueryRow(countQuery, args...).Scan(&total)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Get paginated items
	// Use explicit column list and safe sort parameters (validated above)
	dataQuery := fmt.Sprintf("SELECT id, url, title, source, fetched_at, publish_date FROM announcements WHERE %s ORDER BY %s %s LIMIT ? OFFSET ?", whereClause, sortBy, order)
	args = append(args, limit, offset)

	rows, err := m.DB.Query(dataQuery, args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var history []HistoryItem
	for rows.Next() {
		var item HistoryItem
		var title, source, publishDate sql.NullString
		if err := rows.Scan(&item.ID, &item.URL, &title, &source, &item.FetchedAt, &publishDate); err != nil {
			continue
		}
		item.Title = title.String
		item.Source = source.String
		item.Date = publishDate.String
		history = append(history, item)
	}

	if history == nil {
		history = []HistoryItem{}
	}

	totalPages := (total + limit - 1) / limit
	if totalPages < 1 {
		totalPages = 1
	}

	json.NewEncoder(w).Encode(HistoryResponse{
		Items:      history,
		Total:      total,
		Page:       page,
		TotalPages: totalPages,
	})
}

func main() {
	config := loadConfig()

	if config.WebhookURL == "" {
		fmt.Println("警告: 未设置企业微信 webhook URL")
		fmt.Println("请设置环境变量: export WEBHOOK_URL='你的webhook地址'")
		fmt.Println("程序将继续运行，但不会发送实际通知\n")
	}

	// Initialize DB
	db, err := initDB(config.DBFile)
	if err != nil {
		fmt.Printf("Fatal: Failed to initialize database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()
	fmt.Printf("Database initialized: %s\n", config.DBFile)

	// Migrate legacy history if exists
	migrateHistory(db, "history.txt")

	// Sync targets from config.json to DB (allows manual file edits to take effect)
	syncTargets(db, config)

	// Initialize Monitor
	monitor := &Monitor{
		Config:           config,
		DB:               db,
		ConfigUpdateChan: make(chan bool, 1),
	}

	// Start Web Server
	go func() {
		// Middleware to disable caching for static files
		noCache := func(h http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
				w.Header().Set("Pragma", "no-cache")
				w.Header().Set("Expires", "0")
				h.ServeHTTP(w, r)
			})
		}

		http.HandleFunc("/api/config", monitor.handleConfig)
		http.HandleFunc("/api/history", monitor.handleHistory)
		http.HandleFunc("/api/repush", monitor.handleRepush)
		http.Handle("/", noCache(http.FileServer(http.Dir("./static"))))

		fmt.Printf("Web Dashboard running at http://localhost:8080\n")
		if err := http.ListenAndServe(":8080", nil); err != nil {
			fmt.Printf("Web server error: %v\n", err)
		}
	}()

	fmt.Printf("开始监控 %d 个目标\n", len(config.Targets))
	for _, t := range config.Targets {
		fmt.Printf("- %s: %s\n", t.Name, t.URL)
	}
	fmt.Printf("检查间隔: %d 秒\n", config.CheckInterval)
	fmt.Println("按 Ctrl+C 停止程序\n")

	// Initial check run immediately
	go func() {
		monitor.Mu.RLock()
		tempConfig := &Config{
			Targets:             make([]TargetConfig, len(monitor.Config.Targets)),
			WebhookURL:          monitor.Config.WebhookURL,
			CheckInterval:       monitor.Config.CheckInterval,
			DBFile:              monitor.Config.DBFile,
			DeepSeekAPIKey:      monitor.Config.DeepSeekAPIKey,
			IgnoreOlderThanDays: monitor.Config.IgnoreOlderThanDays,
		}
		// Targets are now fetched from DB inside checkForNewAnnouncements, but we pass config for other fields.
		// Wait, checkForNewAnnouncements queries DB for targets now, so we don't strictly need to copy Targets to tempConfig,
		// but we might need WebhookURL.
		monitor.Mu.RUnlock()

		if err := checkForNewAnnouncements(tempConfig, db); err != nil {
			fmt.Printf("首次检查失败: %v\n", err)
		}
	}()

	// Dynamic Ticker Loop
	timer := time.NewTimer(time.Duration(config.CheckInterval) * time.Second)
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			// Timer fired, run check
			monitor.Mu.RLock()
			currentInterval := monitor.Config.CheckInterval
			tempConfig := &Config{
				WebhookURL:          monitor.Config.WebhookURL, // Only critical field needed
				CheckInterval:       currentInterval,
				IgnoreOlderThanDays: monitor.Config.IgnoreOlderThanDays,
			}
			monitor.Mu.RUnlock()

			if err := checkForNewAnnouncements(tempConfig, db); err != nil {
				fmt.Printf("检查失败: %v\n", err)
			}

			// Reset timer
			timer.Reset(time.Duration(currentInterval) * time.Second)

		case <-monitor.ConfigUpdateChan:
			// Config changed (e.g. new target enabled, or interval changed).
			// Run check immediately as requested.
			fmt.Println("配置更新: 立即触发检查...")

			monitor.Mu.RLock()
			currentInterval := monitor.Config.CheckInterval
			tempConfig := &Config{
				WebhookURL:          monitor.Config.WebhookURL,
				CheckInterval:       currentInterval,
				IgnoreOlderThanDays: monitor.Config.IgnoreOlderThanDays,
			}
			monitor.Mu.RUnlock()

			if err := checkForNewAnnouncements(tempConfig, db); err != nil {
				fmt.Printf("检查失败: %v\n", err)
			}

			// Reset timer to full interval
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(time.Duration(currentInterval) * time.Second)
		}
	}
}
