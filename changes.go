package main

import (
	"database/sql"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	_ "modernc.org/sqlite"
)

const defaultDatabasePath = "./data/aliddns.db"

// DomainChangeRecord records one successful DNS record change.
type DomainChangeRecord struct {
	DomainName string    `json:"domainName"`
	RR         string    `json:"rr"`
	IpAddr     string    `json:"ipAddr"`
	Time       time.Time `json:"time"`
	Desc       string    `json:"desc"`
}

type ChangeStore struct {
	db *sql.DB
}

var changeStore *ChangeStore

func NewChangeStore(path string) (*ChangeStore, error) {
	if path == "" {
		path = defaultDatabasePath
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	store := &ChangeStore{db: db}
	store.db.SetMaxOpenConns(1)
	store.db.SetMaxIdleConns(1)
	if _, err = db.Exec(`PRAGMA busy_timeout = 5000; PRAGMA journal_mode = WAL;`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS domain_change_records (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			domain_name TEXT NOT NULL,
			rr TEXT NOT NULL,
			ip_addr TEXT NOT NULL,
			changed_at INTEGER NOT NULL,
			description TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_domain_change_records_domain_time
			ON domain_change_records (domain_name, changed_at DESC, id DESC);
	`); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *ChangeStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *ChangeStore) Add(record DomainChangeRecord) error {
	_, err := s.db.Exec(`
		INSERT INTO domain_change_records
			(domain_name, rr, ip_addr, changed_at, description)
		VALUES (?, ?, ?, ?, ?)
	`, record.DomainName, record.RR, record.IpAddr, record.Time.UnixNano(), record.Desc)
	return err
}

func (s *ChangeStore) List(domainName, rr string) ([]DomainChangeRecord, error) {
	query := `
		SELECT domain_name, rr, ip_addr, changed_at, description
		FROM domain_change_records
		WHERE (? = '' OR domain_name = ?)
		  AND (? = '' OR rr = ?)
		ORDER BY changed_at DESC, id DESC
	`
	rows, err := s.db.Query(query, domainName, domainName, rr, rr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]DomainChangeRecord, 0)
	for rows.Next() {
		var record DomainChangeRecord
		var changedAt int64
		if err := rows.Scan(&record.DomainName, &record.RR, &record.IpAddr, &changedAt, &record.Desc); err != nil {
			return nil, err
		}
		record.Time = time.Unix(0, changedAt).In(time.Local)
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func (s *ChangeStore) Domains() ([]string, error) {
	rows, err := s.db.Query(`
		SELECT DISTINCT domain_name
		FROM domain_change_records
		ORDER BY domain_name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	domains := make([]string, 0)
	for rows.Next() {
		var domain string
		if err := rows.Scan(&domain); err != nil {
			return nil, err
		}
		domains = append(domains, domain)
	}
	return domains, rows.Err()
}

func saveDomainChange(record DomainChangeRecord) {
	if !recordChanges {
		return
	}
	if changeStore == nil {
		log.Println("变更记录数据库未初始化")
		return
	}
	if err := changeStore.Add(record); err != nil {
		log.Println("保存变更记录失败：", err)
	}
}

func ListDomainChanges(c *gin.Context) {
	if changeStore == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "change store is not initialized"})
		return
	}
	records, err := changeStore.List(c.Query("DomainName"), c.Query("RR"))
	if err != nil {
		log.Println("读取变更记录失败：", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read change records"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": records})
}

type changesPageData struct {
	Domains        []string
	SelectedDomain string
	Records        []DomainChangeRecord
}

var changesPageTemplate = template.Must(template.New("changes").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>DNS 变更记录</title>
  <style>
    :root { color-scheme: light; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    body { margin: 0; background: #f4f6f8; color: #1f2933; }
    main { max-width: 1100px; margin: 0 auto; padding: 32px 20px 48px; }
    header { display: flex; align-items: end; justify-content: space-between; gap: 20px; margin-bottom: 20px; }
    h1 { margin: 0; font-size: 26px; }
    .filter { display: flex; align-items: center; gap: 10px; }
    select { min-width: 240px; padding: 9px 12px; border: 1px solid #c9d1d9; border-radius: 6px; background: #fff; font-size: 14px; }
    .panel { overflow: hidden; border: 1px solid #d8dee4; border-radius: 8px; background: #fff; box-shadow: 0 2px 8px rgba(31, 41, 51, .04); }
    table { width: 100%; border-collapse: collapse; }
    th, td { padding: 13px 16px; border-bottom: 1px solid #edf0f2; text-align: left; vertical-align: top; }
    th { background: #f8fafb; color: #52606d; font-size: 12px; font-weight: 600; letter-spacing: 0; }
    td { font-size: 14px; }
    tbody tr:last-child td { border-bottom: 0; }
    code { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; word-break: break-all; }
    .empty { padding: 54px 20px; color: #7b8794; text-align: center; }
    @media (max-width: 700px) {
      main { padding: 22px 12px 32px; }
      header { display: block; }
      .filter { display: block; margin-top: 16px; }
      .filter label { display: block; margin-bottom: 7px; }
      select { width: 100%; min-width: 0; }
      .panel { overflow-x: auto; }
      table { min-width: 700px; }
    }
  </style>
</head>
<body>
  <main>
    <header>
      <h1>DNS 变更记录</h1>
      <form class="filter" method="get" action="/changes">
        <label for="domain">域名</label>
        <select id="domain" name="DomainName" onchange="this.form.submit()">
          <option value="">全部域名</option>
          {{range .Domains}}<option value="{{.}}" {{if eq . $.SelectedDomain}}selected{{end}}>{{.}}</option>{{end}}
        </select>
      </form>
    </header>
    <section class="panel">
      {{if .Records}}
      <table>
        <thead><tr><th>时间</th><th>域名</th><th>记录</th><th>地址</th><th>变更说明</th></tr></thead>
        <tbody>
        {{range .Records}}
          <tr><td>{{.Time.Format "2006-01-02 15:04:05"}}</td><td>{{.DomainName}}</td><td>{{.RR}}</td><td><code>{{.IpAddr}}</code></td><td>{{.Desc}}</td></tr>
        {{end}}
        </tbody>
      </table>
      {{else}}<div class="empty">暂无变更记录</div>{{end}}
    </section>
  </main>
</body>
</html>`))

func ChangesPage(c *gin.Context) {
	if changeStore == nil {
		c.String(http.StatusInternalServerError, "change store is not initialized")
		return
	}
	selectedDomain := c.Query("DomainName")
	domains, err := changeStore.Domains()
	if err != nil {
		log.Println("读取域名列表失败：", err)
		c.String(http.StatusInternalServerError, "failed to read domains")
		return
	}
	records, err := changeStore.List(selectedDomain, c.Query("RR"))
	if err != nil {
		log.Println("读取变更记录失败：", err)
		c.String(http.StatusInternalServerError, "failed to read change records")
		return
	}
	c.Status(http.StatusOK)
	if err := changesPageTemplate.Execute(c.Writer, changesPageData{
		Domains:        domains,
		SelectedDomain: selectedDomain,
		Records:        records,
	}); err != nil {
		log.Println("渲染变更记录页面失败：", err)
	}
}
