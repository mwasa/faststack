package api

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strings"

	"time"

	"github.com/Sirupsen/logrus"
	"github.com/bamzi/jobrunner"
	"github.com/hyperhq/hyperd/client"
	"github.com/hyperhq/hyperd/client/api"
	"github.com/labstack/echo"
	"github.com/labstack/gommon/log"
)

var (
	Hyper       = api.NewClient("unix", "/var/run/hyper.sock", nil)
	HyperClient = client.NewHyperClient("unix", "/var/run/hyper.sock", nil)
	Images      []Image
	Logger      = logrus.New()
)

type Api struct {
	server *echo.Echo
}

func init() {
	dat, err := ioutil.ReadFile("images/images.json")
	if err != nil {
		Logger.Fatal(err)
	}

	err = json.Unmarshal([]byte(dat), &Images)
	if err != nil {
		Logger.Fatal(err)
	}
}

func New() *Api {

	e := echo.New()
	e.Debug = true
	e.Logger.SetLevel(log.INFO)

	e.Static("/", "app")

	e.POST("/boxes", CreateBox)
	e.GET("/boxes/:id/exec", ExecBox)

	e.Renderer = &Template{
		templates: template.Must(template.ParseGlob("views/*.html")),
	}

	a := &Api{server: e}

	a.server.HTTPErrorHandler = a.ErrorHandler

	return a
}

func (a *Api) Run() {

	jobrunner.Start()
	jobrunner.Schedule("@every 1m", RemoveBoxes{})
	jobrunner.Schedule("@midnight", PullImages{})

	tlscert, tlskey := os.Getenv("TLS_CERT"), os.Getenv("TLS_KEY")
	if tlscert != "" && tlskey != "" {
		Logger.Fatal(a.server.StartTLS(":7842", "cert.pem", "key.pem"))
	} else {
		Logger.Fatal(a.server.Start(":7842"))
	}
}

func (a *Api) ErrorHandler(err error, c echo.Context) {
	httpError, ok := err.(*echo.HTTPError)
	if ok {
		errorCode := httpError.Code
		switch errorCode {
		case http.StatusNotFound:
			err = Index(c)
		default:
			a.server.DefaultHTTPErrorHandler(err, c)
		}
	}
}

func Index(c echo.Context) error {
	return c.Render(http.StatusOK, "index", "")
}

type Template struct {
	templates *template.Template
}

func (t *Template) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

type PullImages struct {
}

func (c PullImages) Run() {
	for _, image := range Images {
		for _, version := range image.Versions {
			imageName := fmt.Sprintf("%s:%s", image.Image, version)
			Logger.Info("Pulling image ", imageName)
			err := HyperClient.PullImage(imageName)
			if err != nil {
				Logger.Error(err)
			}
		}
	}
}

type RemoveBoxes struct {
}

func (c RemoveBoxes) Run() {
	remoteInfo, err := Hyper.List("pod", "", "", true)
	if err != nil {
		Logger.Error(err)
		return
	}

	for _, podData := range remoteInfo.GetList("podData") {
		fields := strings.Split(podData, ":")
		podID, podName := fields[0], fields[1]

		if podName != "termbox" {
			continue
		}

		podInfo, err := Hyper.GetPodInfo(podID)
		if err != nil {
			Logger.Error(err)
			continue
		}

		// Delete pods after 6 hours
		if (time.Now().Unix() - podInfo.CreatedAt) > 21600 {
			err := Hyper.RmPod(podID)
			if err != nil {
				Logger.Error(err)
				continue
			}
			Logger.Info("Deleted ", podID)
		}

	}
}
