package main

import (
"fmt"
"net/http"
)

func main() {
http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
fmt.Fprintf(w, "<h1>Hello world</h1><p>Served locally from Termux on Android 15!</p>")
})
fmt.Println("Server running at http://127.0.0.1:8080 ...")
http.ListenAndServe("127.0.0.1:8080", nil)
}
