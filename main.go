package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
)

type position struct {
	x float32
	y float32
}

const (
	N = iota
	S
	E
	W
)

type Scan struct {
	id   string
	pos  position
	dist []float32
	next []string
}

func downloadScan(id string) (err error) {

	fmt.Print("Downloading " + id + "...")
	// Create the file
	out, err := os.Create(path.Join(wd, id))
	if err != nil {
		return err
	}
	defer out.Close()

	// Get the data
	resp, err := http.Get(baseURL + id)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// save to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	// rewind file
	if _, err = out.Seek(0, io.SeekStart); err != nil {
		return err
	}

	parseScan(id, out)
	mux.Lock()
	scan := scans[id]
	mux.Unlock()
	for _, l := range scan.next {
		if l != "" {
			queueScanDownload(l)
		}
	}

	wg.Done()
	fmt.Println(" Done!")

	return nil
}

func queueScanDownload(id string) {
	mux.Lock()
	_, exists := scans[id]
	if !exists {
		scans[id] = Scan{} // block the spot
	}
	mux.Unlock()
	if !exists {
		wg.Add(1)
		go func() {
			dwQueue <- id
		}() // avoiding deadlock by delegating to yet another coroutine
	}
}

func makeFloat32(s string) float32 {
	f, err := strconv.ParseFloat(s, 32)
	if err != nil {
		panic(err)
	}

	return float32(f)
}

func parseScan(id string, r io.Reader) {
	s := bufio.NewScanner(r)
	s.Scan() // first line with version

	s.Scan() // robot coordinates
	coords := strings.Split(s.Text(), " ")

	x := makeFloat32(coords[0])
	y := makeFloat32(coords[1])

	// distance array
	dist := make([]float32, 36)
	for i := range dist {
		s.Scan()
		dist[i] = makeFloat32(s.Text())
	}

	links := make([]string, 4)
	for i := 0; i < 4; i++ {
		s.Scan()
		line := s.Text()
		l := strings.Split(line, ": ")
		if strings.HasSuffix(l[1], ".txt") {
			switch l[0] {
			case "MOVE_NORTH":
				links[N] = l[1]
			case "MOVE_SOUTH":
				links[S] = l[1]
			case "MOVE_EAST":
				links[E] = l[1]
			case "MOVE_WEST":
				links[W] = l[1]
			}
		}
	}

	mux.Lock()
	scans[id] = Scan{id: id, pos: position{x: x, y: y}, dist: dist, next: links}
	mux.Unlock()
}

func downloadWorker() {
	for {
		select {
		case id := <-dwQueue:
			if err := downloadScan(id); err != nil {
				panic(err)
			}
		case <-dwEnd:
			return
		}
	}
}

var scans map[string]Scan
var mux sync.Mutex
var dwQueue chan string
var dwEnd chan int
var wg sync.WaitGroup

const baseURL = "http://gynvael.coldwind.pl/misja008_drone_io/scans/"
const maxParallel = 40

var wd string

func main() {
	if len(os.Args) < 2 {
		fmt.Println(os.Args[0] + " path [download] [process]")
		return
	}

	wd = os.Args[1]

	cmds := os.Args[2:]
	dw := len(cmds) > 0 && cmds[0] == "download"
	process := len(cmds) == 0 || cmds[0] == "process" || (len(cmds) > 1 && cmds[1] == "process")

	scans = make(map[string]Scan)
	dwQueue = make(chan string, maxParallel*4)

	if dw {
		for i := 0; i < maxParallel; i++ {
			go func() {
				downloadWorker()
			}()
		}

		queueScanDownload("68eb1a7625837e38d55c54dc99257a17.txt")
		wg.Wait()
		dwEnd <- 0
	}

	if process {
		fmt.Println("Processing...")
	}

	fmt.Println("Done ")
}
