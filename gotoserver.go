/*
 This package implements a simple HTTP server that accepts shortcuts, stores
 them to an ad-hoc SQLite database in the running directory, and uses them to
 redirect requests.
 */
package main

import (
	"fmt"
	"http"
	"gosqlite.googlecode.com/hg/sqlite"
)

type LookupRequest struct {
	key string
	reply chan string
}

type SaveRequest struct {
	key string
	url string
	reply chan bool
}

var lookup chan LookupRequest
var save chan SaveRequest

type Redirect struct {
	key string // from
	url string // to
}

const registration = `
<html>
<body>
  Register a link:
  <form method="POST">
    Key: <input name="key"><br>
    Url: <input name="url">
    <input type="submit">
  </form>
</body>
</html>`

func handle(w http.ResponseWriter, r *http.Request) {
	// If root, show the link registration page.
	if r.URL.Path == "/" {
		switch r.Method {
		case "GET":
			w.Write([]byte(registration))

		case "POST":
			// Get input
			key := r.FormValue("key")
			url := r.FormValue("url")

			// Write to db
			resp := make(chan bool)
			save <- SaveRequest{key, url, resp}
			_ = <- resp
			w.Write([]byte("ok"))
		}
		return
	}

	// Redirect user based on the path.
	resp := make(chan string)
	code := r.URL.Path[1:]
	lookup <- LookupRequest{code, resp}
	url := <- resp
	if url == "" {
		http.Error(w, "Key not found", http.StatusNotFound)
		return
	}
	http.Redirect(w, r, <- resp, http.StatusFound)
}

/** This function performs the db lookup. */
func doLookup(c *sqlite.Conn, key string) (url string) {
	stmt, err := c.Prepare("select url from redirects where key = ?")
	if err != nil {
		panic(err.String())
	}
	defer stmt.Finalize()
	stmt.Exec(key)
	stmt.Next()
	stmt.Scan(&url)
	return
}

func main() {
	// Open/create the DB
	conn, err := sqlite.Open("goserver.db")
	if err != nil {
		panic(err.String())
	}
	defer conn.Close()

	// If our table doesn't exist create it.
	err = conn.Exec(`
create table if not exists redirects
 (key varchar(32) not null,
  url varchar(255) not null)`)
	if err != nil {
		panic(err.String())
	}

	// Create the channels that handler threads use to make db requests.
	// (SQLite is not threadsafe)
	lookup = make(chan LookupRequest)
	save = make(chan SaveRequest)

	// Run a goroutine to serialize requests to the db
	go func() {
		for {
			select {
			case req := <- lookup:
				req.reply <- doLookup(conn, req.key)
			case req := <- save:
				conn.Exec("delete from redirects where key = ?", req.key)
				conn.Exec("replace into redirects values (?, ?)", req.key, req.url)
				req.reply <- true
			}
		}
	}()

	// Serve pages
	http.HandleFunc("/", handle)
	http.ListenAndServe(":8080", nil)
}
