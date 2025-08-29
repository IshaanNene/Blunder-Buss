/*
 * Copyright (c) 2025 Ishaan Nene
 *
 * This source code is licensed under the MIT license found in the
 * LICENSE file in the root directory of this source tree.
 */
/*
This file handles client side interaction with the API. It sends a sample request to the API and prints the response.
Note: This is just a sample client. You can use curl or Postman to interact with the API as well.
*/
package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net/http"
    "time"
)

func main() {
    url := "http://localhost:30080/analyze" 
    job := map[string]interface{}{"fen": "", "max_time_ms": 1500}
    b, _ := json.Marshal(job)
    resp, err := http.Post(url, "application/json", bytes.NewReader(b))
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    body, _ := io.ReadAll(resp.Body)
    fmt.Println("response:", string(body))
    fmt.Println("waiting 2s...")
    time.Sleep(2 * time.Second)
    fmt.Println("done")
}