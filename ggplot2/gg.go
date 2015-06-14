package ggplot2

import (
	"os"
	"os/exec"
)

// Responsible for running R and get output as ggplot2 plot
type Gg struct {
	Dir  string
	Type string
}

func (gg *Gg) ImgName() string {
	return "img." + gg.Type
}

func (gg *Gg) AddFile(name string, content string) (err error) {
	f, err := os.OpenFile(gg.Dir+"/"+name, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, err = f.WriteString(content)
	return
}

func (gg *Gg) AddCode(code string) (err error) {
	prog := "library(ggplot2);" +
		"\n" +
		code +
		"\n" +
		`ggsave(file="` + gg.ImgName() + `", dpi=72, width=8, height=8);`

	return gg.AddFile("program.R", prog)
}

func (gg *Gg) Run() (string, error) {
	cmd := exec.Command("docker", "run", "-v", gg.Dir+":/tmp", "--workdir", "/tmp", "quay.io/edvakf/r-ggplot2", "R", "--vanilla", "--quiet", "-f", "program.R")

	out, err := cmd.CombinedOutput()
	return string(out), err
}
