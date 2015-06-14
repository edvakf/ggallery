package models

import (
	"errors"
	"regexp"
	"time"

	"github.com/VividCortex/mysqlerr"
	"github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"

	"github.com/edvakf/ggallery/ggplot2"
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

// plot table
type plot struct {
	ID        string    `db:"id"`
	CreatedAt time.Time `db:"created_at"`
	Code      string    `db:"code"`
}

// file table
type file struct {
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

func selectPlot(id string) (p plot, err error) {
	err = db.Get(&p, `SELECT * FROM plot WHERE id = ?`, id)
	return
}

func selectFiles(plotID string) (files []file, err error) {
	err = db.Select(&files, `SELECT * FROM file WHERE plot_id = ?`, plotID)
	return
}

func SelectPlotAndFiles(id string) (pd *PlotData, err error) {
	p, err := selectPlot(id)
	if err != nil {
		return
	}
	files, err := selectFiles(id)
	if err != nil {
		return
	}
	pd = &PlotData{Code: p.Code, Files: map[string]string{}}
	for _, file := range files {
		pd.Files[file.Name] = file.Content
	}
	return
}

func ExecPlot(dir string, pd *PlotData) (out string, imgFile string, err error) {
	gg := ggplot2.Gg{Dir: dir, Type: "svg"}

	for name, content := range pd.Files {
		err = gg.AddFile(name, content)
		if err != nil {
			return
		}
	}

	gg.AddCode(pd.Code)

	out, err = gg.Run()
	if err != nil {
		return
	}
	imgFile = dir + "/" + gg.ImgName()
	return
}

var re = regexp.MustCompile("^[0-9a-zA-Z_]+$")

func ValidateFileNames(files map[string]string) error {
	for name, _ := range files {
		if !re.MatchString(name) {
			return errors.New("File name can contain only characters in [0-9a-zA-Z_]")
		}
	}
	return nil
}
