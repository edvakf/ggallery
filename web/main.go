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
	"strings"

	"github.com/zenazn/goji"
	"github.com/zenazn/goji/web"

	"github.com/edvakf/ggallery/models"
	"github.com/edvakf/ggallery/util"
)

var dbHostPort string
var tmpDirBase string
var viewsDir string
var staticDir string

func init() {
	flag.StringVar(&dbHostPort, "db", "127.0.0.1:3306", "MySQL host and port")
	flag.StringVar(&tmpDirBase, "tmpdir", "/tmp", "tmp directory. when using boot2docker, directories outside of /Users are not mountable, so use ~/tmp for instance.")
	flag.StringVar(&viewsDir, "views", "", "web views directory")
	flag.StringVar(&staticDir, "static", "", "static file directory")
	flag.Parse()

	if !util.IsDirectory(viewsDir) {
		panic("You must pass an existing directory to the `views` option")
	}
	if !util.IsDirectory(staticDir) {
		panic("You must pass an existing directory to the `static` option")
	}

	models.InitDB(dbHostPort)
}

func main() {
	goji.Post("/plot", APIHandler(postPlotHandler))
	goji.Post("/run", APIHandler(runHandler))
	goji.Post("/replot", APIHandler(runHandler))
	goji.Get(regexp.MustCompile(`^/plot/(?P<id>[0-9a-zA-Z]+)$`), APIHandler(getPlotHandler))
	goji.Get(regexp.MustCompile(`^/plot/(?P<id>[0-9a-zA-Z]+).svg$`), APIHandler(getPlotImageHandler))
	goji.Get(regexp.MustCompile(`^/edit/[0-9a-zA-Z]+$`), func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, viewsDir+"/edit.html") })
	goji.Get("/edit", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, viewsDir+"/edit.html") })
	goji.Get("/", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, viewsDir+"/index.html") })
	goji.Get("/static/*", http.StripPrefix("/static", http.FileServer(http.Dir(staticDir))))
	goji.Serve()
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
}

// API error response
type ErrorResponse struct {
	Error  string `json:"error"`  // error message
	Output string `json:"output"` // output of R
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

type ErrHandlerFunc func(c web.C, w http.ResponseWriter, r *http.Request) error

// goji accepts handler of type `goji.HandlerFunc` which is actually
// `func(c web.C, w http.ResponseWriter, r *http.Request)`
// APIHandler excecutes my own handler `ErrHandlerFunc` and converts that to `goji.HandlerFunc`
// if the ErrHandlerFunc returns an error, it logs and output an InternalServerError
// anything other than InternalServerError must be responded by ErrHandlerFunc
func APIHandler(handler ErrHandlerFunc) func(c web.C, w http.ResponseWriter, r *http.Request) {
	return func(c web.C, w http.ResponseWriter, r *http.Request) {
		err := handler(c, w, r)

		if err != nil {
			log.Println(err.Error())
			http.Error(w, ApiErrorJSON("Internal Server Error", ""), http.StatusInternalServerError)
		}
	}
}

func processPostPlotBody(r *http.Request) (*models.PlotData, error) {
	var pd models.PlotData
	if r.Header.Get("Content-Type") == "application/json" {
		err := json.NewDecoder(r.Body).Decode(&pd)
		if err != nil {
			return nil, errors.New("Request JSON invalid")
		}
	} else {
		return nil, errors.New("Content-Type other than application/json not supported yet")
	}

	if strings.Trim(pd.Code, " ") == "" {
		return nil, errors.New("Cannot execute empty code")
	}
	err := models.ValidateFileNames(pd.Files)
	if err != nil {
		return nil, err
	}
	return &pd, nil
}

func processReplotBody(r *http.Request) (*models.ReplotData, error) {
	var rd models.ReplotData
	if r.Header.Get("Content-Type") == "application/json" {
		err := json.NewDecoder(r.Body).Decode(&rd)
		if err != nil {
			return nil, errors.New("Request JSON invalid")
		}
	} else {
		return nil, errors.New("Content-Type other than application/json not supported yet")
	}

	err := models.ValidateFileNames(rd.Files)
	if err != nil {
		return nil, err
	}
	return &rd, nil
}

func postPlotHandler(c web.C, w http.ResponseWriter, r *http.Request) error {
	pd, err := processPostPlotBody(r)
	if err != nil {
		http.Error(w, ApiErrorJSON(err.Error(), ""), http.StatusBadRequest)
		return nil
	}

	// make tmpDir
	tmpDir, err := ioutil.TempDir(tmpDirBase, "")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	out, imgFile, err := models.ExecPlot(tmpDir, pd)
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

	id, err := models.InsertPlotAndFiles(pd)
	if err != nil {
		return err
	}

	err = json.NewEncoder(w).Encode(PlotResponse{Output: out, SVG: string(svg), ID: id})
	if err != nil {
		return err
	}
	return nil
}

func runHandler(c web.C, w http.ResponseWriter, r *http.Request) error {
	pd, err := processPostPlotBody(r)
	if err != nil {
		http.Error(w, ApiErrorJSON(err.Error(), ""), http.StatusBadRequest)
		return nil
	}

	// make tmpDir
	tmpDir, err := ioutil.TempDir(tmpDirBase, "")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	out, imgFile, err := models.ExecPlot(tmpDir, pd)
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
}

func getPlotHandler(c web.C, w http.ResponseWriter, r *http.Request) error {
	id := c.URLParams["id"]

	pd, err := models.SelectPlotAndFiles(id)
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
}

func getPlotImageHandler(c web.C, w http.ResponseWriter, r *http.Request) error {
	id := c.URLParams["id"]

	pd, err := models.SelectPlotAndFiles(id)
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

	_, imgFile, err := models.ExecPlot(tmpDir, pd)
	if err != nil {
		// unlike POST /plot API, code excecution failure causes 500 error here instead of 422
		// because the fact that the code is stored already means it was once able to be run
		return err
	}

	http.ServeFile(w, r, imgFile)
	return nil
}

func replotHandler(c web.C, w http.ResponseWriter, r *http.Request) error {
	rd, err := processReplotBody(r)
	if err != nil {
		http.Error(w, ApiErrorJSON(err.Error(), ""), http.StatusBadRequest)
		return nil
	}

	pd, err := models.SelectPlotAndFiles(rd.ID)
	if err == sql.ErrNoRows {
		http.Error(w, ApiErrorJSON("Not found", ""), http.StatusNotFound)
		return nil
	} else if err != nil {
		return err
	}

	for name, content := range rd.Files {
		if _, ok := pd.Files[name]; !ok {
			http.Error(w, ApiErrorJSON("File name must match that of the original plot", ""), http.StatusBadRequest)
			return nil
		}
		// override the file content
		pd.Files[name] = content
	}

	tmpDir, err := ioutil.TempDir(tmpDirBase, "")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	out, imgFile, err := models.ExecPlot(tmpDir, pd)
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

	id, err := models.InsertPlotAndFiles(pd)
	if err != nil {
		return err
	}

	err = json.NewEncoder(w).Encode(PlotResponse{Output: out, SVG: string(svg), ID: id})
	if err != nil {
		return err
	}
	return nil
}
