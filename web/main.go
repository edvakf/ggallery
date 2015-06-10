package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"time"

	"github.com/VividCortex/mysqlerr"
	"github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"github.com/zenazn/goji"
	"github.com/zenazn/goji/web"

	"github.com/edvakf/ggallery/ggplot2"
	"github.com/edvakf/ggallery/util"
)

var db *sqlx.DB
var dbHostPort string
var tmpDirBase string

func init() {
	flag.StringVar(&dbHostPort, "db", "127.0.0.1:3306", "MySQL host and port")
	flag.StringVar(&tmpDirBase, "tmpdir", "/tmp", "tmp directory. when using boot2docker, directories outside of /Users are not mountable, so use ~/tmp for instance.")
	flag.Parse()
}

func main() {
	db = sqlx.MustOpen("mysql", "ggallery:galeria@tcp("+dbHostPort+")/ggallery?charset=utf8&parseTime=True")

	goji.Post("/plot", postPlotHandler)
	goji.Post("/run", runHandler)
	goji.Post("/replot", runHandler)
	goji.Get(regexp.MustCompile(`^/plot/(?P<id>[0-9a-zA-Z]+)$`), getPlotHandler)
	goji.Get(regexp.MustCompile(`^/plot/(?P<id>[0-9a-zA-Z]+).svg$`), getPlotImageHandler)
	goji.Serve()
}

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

// API response JSON of /run
type RunResponse struct {
	Output string `json:"output"`
	SVG    string `json:"svg"`
}

// API response JSON of /plot (POST) and /replot
type PlotResponse struct {
	Output string `json:"output"`
	SVG    string `json:"svg"`
	ID     string `json:"id"`
	SVGURL string `json:"svg_url"`
}

// API error response
type ErrorResponse struct {
	Error  string `json:"error"`  // error message
	Output string `json:"output"` // output of R
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

const PlotIDLen = 5

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

type ApiError struct {
	Error  string `json:"error"`  // error message
	Output string `json:"output"` // combined output of stdout and stderr from R
}

func ApiErrorJSON(msg string, out string) string {
	j, err := json.Marshal(ApiError{Error: msg, Output: out})
	if err != nil {
		return ""
	}
	return string(j)
}

func processPostPlotBody(r *http.Request) (*PlotData, error) {
	var pd PlotData
	if r.Header.Get("Content-Type") == "application/json" {
		err := json.NewDecoder(r.Body).Decode(&pd)
		if err != nil {
			return nil, errors.New("Request JSON invalid")
		}
	} else {
		return nil, errors.New("Content-Type other than application/json not supported yet")
	}

	// check file names
	re := regexp.MustCompile("[^0-9a-zA-Z_]")
	for name, _ := range pd.Files {
		if re.Match([]byte(name)) {
			return nil, errors.New("File name must not contain letters other than [0-9a-zA-Z_]")
		}
	}
	return &pd, nil
}

func processReplotBody(r *http.Request) (rd *ReplotData, err error) {
	if r.Header.Get("Content-Type") == "application/json" {
		err = json.NewDecoder(r.Body).Decode(rd)
		if err != nil {
			return nil, errors.New("Request JSON invalid")
		}
	} else {
		return nil, errors.New("Content-Type other than application/json not supported yet")
	}

	// check file names
	re := regexp.MustCompile("[^0-9a-zA-Z_]")
	for name, _ := range rd.Files {
		if re.Match([]byte(name)) {
			return nil, errors.New("File name must not contain letters other than [0-9a-zA-Z_]")
		}
	}
	return rd, nil
}

func plot(dir string, pd *PlotData) (out string, imgFile string, err error) {
	// plot
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

func postPlotHandler(c web.C, w http.ResponseWriter, r *http.Request) {
	// only 500 errors are handled outside
	unhandledErr := func() error {

		pd, err := processPostPlotBody(r)
		if err != nil {
			http.Error(w, ApiErrorJSON("Invalid request format", ""), http.StatusBadRequest)
			return nil
		}

		// make tmpDir
		tmpDir, err := ioutil.TempDir(tmpDirBase, "")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmpDir)

		out, imgFile, err := plot(tmpDir, pd)
		if err != nil {
			if _, ok := err.(*exec.ExitError); ok {
				http.Error(w, ApiErrorJSON("Program failed to excecute", out), 422) // Unprocessable Entity
				return nil
			}
			return err
		}

		svg, err := ioutil.ReadFile(imgFile)
		if err != nil {
			return err
		}

		id, err := InsertPlotAndFiles(pd)
		if err != nil {
			return err
		}

		err = json.NewEncoder(w).Encode(PlotResponse{Output: out, SVG: string(svg), ID: id, SVGURL: "/plot/" + id + ".svg"})
		if err != nil {
			return err
		}
		return nil
	}()

	if unhandledErr != nil {
		log.Println(unhandledErr.Error())
		http.Error(w, ApiErrorJSON("Internal Server Error", ""), http.StatusInternalServerError)
	}
}

func runHandler(c web.C, w http.ResponseWriter, r *http.Request) {
	// only 500 errors are handled outside
	unhandledErr := func() error {

		pd, err := processPostPlotBody(r)
		if err != nil {
			http.Error(w, ApiErrorJSON("Invalid request format", ""), http.StatusBadRequest)
			return nil
		}

		// make tmpDir
		tmpDir, err := ioutil.TempDir(tmpDirBase, "")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmpDir)

		out, imgFile, err := plot(tmpDir, pd)
		if err != nil {
			if _, ok := err.(*exec.ExitError); ok {
				http.Error(w, ApiErrorJSON("Program failed to excecute", out), 422) // Unprocessable Entity
				return nil
			}
			return err
		}

		svg, err := ioutil.ReadFile(imgFile)
		if err != nil {
			return err
		}

		err = json.NewEncoder(w).Encode(RunResponse{Output: out, SVG: string(svg)})
		if err != nil {
			return err
		}
		return nil
	}()

	if unhandledErr != nil {
		log.Println(unhandledErr.Error())
		http.Error(w, ApiErrorJSON("Internal Server Error", ""), http.StatusInternalServerError)
	}
}

func getPlotHandler(c web.C, w http.ResponseWriter, r *http.Request) {
	// only 500 errors are handled outside
	unhandledErr := func() error {

		id := c.URLParams["id"]

		pd, err := SelectPlotAndFiles(id)
		if err == sql.ErrNoRows {
			http.Error(w, ApiErrorJSON("Not found", ""), http.StatusNotFound)
			return nil
		} else if err != nil {
			return err
		}

		err = json.NewEncoder(w).Encode(pd)
		if err != nil {
			return err
		}
		return nil
	}()

	if unhandledErr != nil {
		log.Println(unhandledErr.Error())
		http.Error(w, ApiErrorJSON("Internal Server Error", ""), http.StatusInternalServerError)
	}
}

func getPlotImageHandler(c web.C, w http.ResponseWriter, r *http.Request) {
	unhandledErr := func() error {

		id := c.URLParams["id"]

		pd, err := SelectPlotAndFiles(id)
		if err == sql.ErrNoRows {
			http.Error(w, ApiErrorJSON("Not found", ""), http.StatusNotFound)
			return nil
		} else if err != nil {
			return err
		}

		// make tmpDir
		tmpDir, err := ioutil.TempDir(tmpDirBase, "")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmpDir)

		_, imgFile, err := plot(tmpDir, pd)
		if err != nil {
			// unlike POST /plot API, code excecution failure causes 500 error here instead of 422
			// because the fact that the code is stored already means it was once able to be run
			return err
		}

		http.ServeFile(w, r, imgFile)
		return nil
	}()

	if unhandledErr != nil {
		log.Println(unhandledErr.Error())
		http.Error(w, ApiErrorJSON("Internal Server Error", ""), http.StatusInternalServerError)
	}
}

func replotHandler(c web.C, w http.ResponseWriter, r *http.Request) {
	// only 500 errors are handled outside
	unhandledErr := func() error {

		rd, err := processReplotBody(r)
		if err != nil {
			http.Error(w, ApiErrorJSON("Invalid request format", ""), http.StatusBadRequest)
			return nil
		}

		p, err := SelectPlot(rd.ID)
		if err == sql.ErrNoRows {
			http.Error(w, ApiErrorJSON("Not found", ""), http.StatusNotFound)
			return nil
		} else if err != nil {
			return err
		}

		pd := &PlotData{Code: p.Code, Files: rd.Files}

		tmpDir, err := ioutil.TempDir(tmpDirBase, "")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmpDir)

		out, imgFile, err := plot(tmpDir, pd)
		if err != nil {
			if _, ok := err.(*exec.ExitError); ok {
				http.Error(w, ApiErrorJSON("Program failed to excecute", out), 422) // Unprocessable Entity
				return nil
			}
			return err
		}

		svg, err := ioutil.ReadFile(imgFile)
		if err != nil {
			return err
		}

		id, err := InsertPlotAndFiles(pd)
		if err != nil {
			return err
		}

		err = json.NewEncoder(w).Encode(PlotResponse{Output: out, SVG: string(svg), ID: id, SVGURL: "/plot/" + id + ".svg"})
		if err != nil {
			return err
		}
		return nil
	}()

	if unhandledErr != nil {
		log.Println(unhandledErr.Error())
		http.Error(w, ApiErrorJSON("Internal Server Error", ""), http.StatusInternalServerError)
	}
}
