package main

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type point struct {
	x float64
	y float64
}

const (
	N = iota
	S
	E
	W
)

type Scan struct {
	id   string
	pos  point
	dist []float64
	next []string
}

func downloadScan(id string, dest string) (err error) {

	fmt.Print("Downloading " + id + "...")
	// Create the file
	out, err := os.Create(path.Join(dest, id))
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

func makeFloat64(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		panic(err)
	}

	return f
}

func parseScanFile(id string, dir string) {
	if s, err := os.Open(path.Join(dir, id)); err == nil {
		defer s.Close()
		parseScan(id, s)
	} else {
		panic(err)
	}
}

func parseScan(id string, r io.Reader) {
	s := bufio.NewScanner(r)
	s.Scan() // first line with version

	s.Scan() // robot coordinates
	coords := strings.Split(s.Text(), " ")

	x := makeFloat64(coords[0])
	y := makeFloat64(coords[1])

	// distance array
	dist := make([]float64, 36)
	for i := range dist {
		s.Scan()
		dist[i] = makeFloat64(s.Text())
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
	scans[id] = Scan{id: id, pos: point{x: x, y: y}, dist: dist, next: links}
	mux.Unlock()
}

func downloadWorker(dest string) {
	for {
		select {
		case id := <-dwQueue:
			if err := downloadScan(id, dest); err != nil {
				panic(err)
			}
		case <-dwEnd:
			return
		}
	}
}

func addScans(dir string, ch chan string) int {
	l, err := filepath.Glob(filepath.Join(dir, "*.txt"))
	if err != nil {
		panic(err)
	}
	for _, f := range l {
		ch <- filepath.Base(f)
	}
	return len(l)
}

func parserWorker(ch chan string, dir string) {
	for id := range ch {
		parseScanFile(id, dir)
		fmt.Print(".")
	}
}

var scans map[string]Scan
var mux sync.Mutex
var dwQueue chan string
var dwEnd chan int
var wg sync.WaitGroup

const baseURL = "http://gynvael.coldwind.pl/misja008_drone_io/scans/"
const maxParallel = 40

func main() {
	if len(os.Args) < 2 {
		fmt.Println(os.Args[0] + " path [download] [process]")
		return
	}

	wd := os.Args[1]

	cmds := os.Args[2:]
	dw := len(cmds) > 0 && cmds[0] == "download"
	process := len(cmds) == 0 || cmds[0] == "process" || (len(cmds) > 1 && cmds[1] == "process")

	scans = make(map[string]Scan)
	dwQueue = make(chan string, maxParallel*4)

	if dw {
		for i := 0; i < maxParallel; i++ {
			go func() {
				downloadWorker(wd)
			}()
		}

		queueScanDownload("68eb1a7625837e38d55c54dc99257a17.txt")
		wg.Wait()

		for i := 0; i < maxParallel; i++ {
			dwEnd <- 0
		}
	}

	if process && len(scans) == 0 {
		fmt.Println("Processing...")

		wg.Add(maxParallel)
		ch := make(chan string, maxParallel*4)
		for i := 0; i < maxParallel; i++ {
			go func() {
				parserWorker(ch, wd)
				wg.Done()
			}()
		}

		cnt := addScans(wd, ch)
		close(ch)

		wg.Wait()
		fmt.Println("\nParsed ", cnt, " scans")
	}

	pts := make([]point, len(scans)*36) // max points
	if process {
		rad := float64(math.Pi / 180.0)
		type sincos struct {
			sin float64
			cos float64
		}
		sc := make([]sincos, 36)
		for i := 0; i < 36; i++ {
			sin, cos := math.Sincos(rad * float64(i*10))
			sc[i] = sincos{sin, cos}
		}

		var mx float64 = 0.0
		var my float64 = 0.0

		factor := 1.0
		pos := 0
		for _, s := range scans {
			for i, d := range s.dist {
				if !math.IsInf(d, 0) {
					p := point{
						x: factor * (s.pos.x + sc[i].sin*d),
						y: factor * (s.pos.y - sc[i].cos*d),
					}
					pts[pos] = p
					pos++
					if p.x > mx {
						mx = p.x
					}
					if p.y > my {
						my = p.y
					}
				}
			}
		}

		w := int(mx + 10)
		h := int(my + 10)
		fmt.Println("w: ", w, " h: ", h)
		out := make([]byte, w*h)
		for _, p := range pts {
			out[int(int(p.y)*w+int(p.x))] = byte(255)
		}

		err := ioutil.WriteFile("/tmp/map.data", out, 0644)
		if err != nil {
			panic(err)
		}
	}

	fmt.Println("Done")
}
