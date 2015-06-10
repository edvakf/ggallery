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

	"github.com/zenazn/goji"
	"github.com/zenazn/goji/web"

	"github.com/edvakf/ggallery/ggplot2"
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
	goji.Post("/plot", postPlotHandler)
	goji.Post("/run", runHandler)
	goji.Post("/replot", runHandler)
	goji.Get(regexp.MustCompile(`^/plot/(?P<id>[0-9a-zA-Z]+)$`), getPlotHandler)
	goji.Get(regexp.MustCompile(`^/plot/(?P<id>[0-9a-zA-Z]+).svg$`), getPlotImageHandler)
	goji.Get("/edit/[0-9a-zA-Z]+", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, viewsDir+"/edit.html") })
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
	SVGURL string `json:"svg_url"`
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

	// check file names
	re := regexp.MustCompile("[^0-9a-zA-Z_]")
	for name, _ := range pd.Files {
		if re.Match([]byte(name)) {
			return nil, errors.New("File name must not contain letters other than [0-9a-zA-Z_]")
		}
	}
	return &pd, nil
}

func processReplotBody(r *http.Request) (rd *models.ReplotData, err error) {
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

func plot(dir string, pd *models.PlotData) (out string, imgFile string, err error) {
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

		id, err := models.InsertPlotAndFiles(pd)
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
	}()

	if unhandledErr != nil {
		log.Println(unhandledErr.Error())
		http.Error(w, ApiErrorJSON("Internal Server Error", ""), http.StatusInternalServerError)
	}
}

func getPlotImageHandler(c web.C, w http.ResponseWriter, r *http.Request) {
	unhandledErr := func() error {

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

		p, err := models.SelectPlot(rd.ID)
		if err == sql.ErrNoRows {
			http.Error(w, ApiErrorJSON("Not found", ""), http.StatusNotFound)
			return nil
		} else if err != nil {
			return err
		}

		pd := &models.PlotData{Code: p.Code, Files: rd.Files}

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

		id, err := models.InsertPlotAndFiles(pd)
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
