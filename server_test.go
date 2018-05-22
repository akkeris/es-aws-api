package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/go-martini/martini"
	"github.com/martini-contrib/binding"
	"github.com/martini-contrib/render"
	. "github.com/smartystreets/goconvey/convey"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func Server() *martini.ClassicMartini {
	m := martini.Classic()
	m.Use(render.Renderer())
	m.Get("/v1/es/plans", plans_handler)
	m.Get("/v1/es/url/:domainname", status_handler)
	m.Get("/v1/es/instance/:domainname/status", status_handler)
	m.Post("/v1/es/tag", binding.Json(tagspec{}), tag_handler)
	return m
}

func Init() *martini.ClassicMartini {
	setenv()
	err := createdb()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	m := Server()
	return m
}

func TestBroker(t *testing.T) {
	m := Init()
	Convey("Given we want the plans\n", t, func() {
		r, _ := http.NewRequest("GET", "/v1/es/plans", nil)
		w := httptest.NewRecorder()
		m.ServeHTTP(w, r)
		So(w.Code, ShouldEqual, http.StatusOK)
		fmt.Println(w)
		decoder := json.NewDecoder(w.Body)
		var response map[string]string
		if err := decoder.Decode(&response); err != nil {
			panic(err)
		}
		So(response["micro"], ShouldNotEqual, "")
		Convey("To get the url\n", func() {
			r, _ := http.NewRequest("GET", "/v1/es/url/merpderp", nil)
			w := httptest.NewRecorder()
			m.ServeHTTP(w, r)
			So(w.Code, ShouldEqual, http.StatusOK)
			fmt.Println(w)
			decoder := json.NewDecoder(w.Body)
			var response map[string]string
			if err := decoder.Decode(&response); err != nil {
				panic(err)
			}
			So(response["ES_URL"], ShouldEqual, "https://vpc-merpderp-266sk4si4qhodjybbn3gbkznpq.us-west-2.es.amazonaws.com")
			So(response["KIBANA_URL"], ShouldEqual, "https://vpc-merpderp-266sk4si4qhodjybbn3gbkznpq.us-west-2.es.amazonaws.com/_plugin/kibana")
			Convey("To get the status\n", func() {
				r, _ := http.NewRequest("GET", "/v1/es/instance/merpderp/status", nil)
				w := httptest.NewRecorder()
				m.ServeHTTP(w, r)
				So(w.Code, ShouldEqual, http.StatusOK)
				fmt.Println(w)
				decoder := json.NewDecoder(w.Body)
				var response map[string]string
				if err := decoder.Decode(&response); err != nil {
					panic(err)
				}
				So(response["ES_URL"], ShouldEqual, "https://vpc-merpderp-266sk4si4qhodjybbn3gbkznpq.us-west-2.es.amazonaws.com")
				So(response["KIBANA_URL"], ShouldEqual, "https://vpc-merpderp-266sk4si4qhodjybbn3gbkznpq.us-west-2.es.amazonaws.com/_plugin/kibana")
				Convey("Tag and instance", func() {
					b := new(bytes.Buffer)
					var payload tagspec
					payload.Resource = "merpderp"
					payload.Name = "unittestname"
					payload.Value = "unittestvalue"
					if err := json.NewEncoder(b).Encode(payload); err != nil {
						panic(err)
					}
					fmt.Println(b)
					req, _ := http.NewRequest("POST", "/v1/es/tag", b)
					resp := httptest.NewRecorder()
					m.ServeHTTP(resp, req)
					fmt.Println(resp)
					So(resp.Code, ShouldEqual, http.StatusCreated)
					decoder := json.NewDecoder(resp.Body)
					var response map[string]string
					if err := decoder.Decode(&response); err != nil {
						panic(err)
					}
					fmt.Println(response)
					So(response["response"], ShouldEqual, "tag added")
				})
			})
		})

	})
}
