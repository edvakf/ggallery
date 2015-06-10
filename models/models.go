package models

import (
	"time"

	"github.com/VividCortex/mysqlerr"
	"github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"

	"github.com/edvakf/ggallery/util"
)

const PlotIDLen = 5

var db *sqlx.DB

// request body JSON used for /plot (POST) and /run
// also response body of /plot (GET)
type PlotData struct {
	Code  string            `json:"code"`
	Files map[string]string `json:"files"`
}

// request body JSON used for /replot
type ReplotData struct {
	ID    string            `json:"id"`
	Files map[string]string `json:"files"`
}

// plot table
type Plot struct {
	ID        string    `db:"id"`
	CreatedAt time.Time `db:"created_at"`
	Code      string    `db:"code"`
}

// file table
type File struct {
	PlotId  string `db:"plot_id"`
	Name    string `db:"name"`
	Content string `db:"content"`
}

func InitDB(hostPort string) {
	db = sqlx.MustOpen("mysql", "ggallery:galeria@tcp("+hostPort+")/ggallery?charset=utf8&parseTime=True")
}

// create plot
func InsertPlotAndFiles(pd *PlotData) (id string, err error) {
	for retry := 3; retry > 0; retry-- {
		id = util.RandomAlphaNum(PlotIDLen)

		_, err = db.Exec(`INSERT INTO plot (id, code) VALUES(?,?)`, id, pd.Code)
		if err != nil {
			if retry > 0 {
				if mysqlError, ok := err.(*mysql.MySQLError); ok {
					if mysqlError.Number == mysqlerr.ER_DUP_ENTRY { // Error 1062: Duplicate entry '12345' for key 'PRIMARY'
						continue // try again
					}
				}
			}
			return
		}
		break
	}

	for name, content := range pd.Files {
		_, err = db.Exec(`INSERT INTO file (plot_id, name, content) VALUES(?,?,?)`, id, name, content)
		if err != nil {
			return
		}
	}

	return
}

func SelectPlot(id string) (p Plot, err error) {
	err = db.Get(&p, `SELECT * FROM plot WHERE id = ?`, id)
	return
}

func SelectFiles(plotID string) (files []File, err error) {
	err = db.Select(&files, `SELECT * FROM file WHERE plot_id = ?`, plotID)
	return
}

func SelectPlotAndFiles(id string) (pd *PlotData, err error) {
	p, err := SelectPlot(id)
	if err != nil {
		return
	}
	files, err := SelectFiles(id)
	if err != nil {
		return
	}
	pd = &PlotData{Code: p.Code}
	for _, file := range files {
		pd.Files[file.Name] = file.Content
	}
	return
}