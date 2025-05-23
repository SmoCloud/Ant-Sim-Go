package main

import (
	"fmt"
	"log"

	// "math"
	"math/rand"
	"runtime"
	"strings"

	"sync"
	"time"

	"github.com/go-gl/gl/v4.6-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
)

func init() {
	runtime.LockOSThread()
}

const (
	Alpha      = 0.65
	Beta       = 0.95
	Gamma      = 0.002 // decay rate of pheromones
	DecayAfter = 60    // cycles before decay begins
	NumAnts    = 20
	Fps        = 10

	VertexShaderSource = `
        #version 410
        in vec3 vp;
        void main() {
            gl_Position = vec4(vp, 1.0);
        }
    ` + "\x00"

	FragmentShaderSource = `
        #version 410
        out vec4 frag_colour;
		uniform vec4 sprite_colour;
        void main() {
            frag_colour = sprite_colour;
        }
    ` + "\x00"
)

var (
	Square = []float32{
		-0.5, 0.5, 0, // top   X, Y, Z
		-0.5, -0.5, 0, // left  X, Y, Z
		0.5, -0.5, 0, // right X, Y, Z

		0.5, -0.5, 0,
		0.5, 0.5, 0,
		-0.5, 0.5, 0,
	}

	AntColours  = make([]float32, 3)
	NestColours = make([]float32, 3)
	FoodColours = make([]float32, 3)

	GridWidth  = 500
	GridHeight = 500
	Rows       = 100
	Cols       = 100
)

type Pair struct { // copilot advised using a struct to create a pair since Go doesn't have built-in tuple types
	X int
	Y int
}

type Vertex struct {
	V Pair
}

type Edge struct {
	Destination Pair
	Weight      *float32
}

type Graph struct {
	Vertices []Vertex
	Edges    map[Pair][]Edge
}

// this function adds a vertex to the vertex array of a graph
func (g *Graph) AddVertex(vtex Pair) {
	g.Vertices = append(g.Vertices, Vertex{V: vtex})
}

// this function appends a new Edge (a vertex and its weight) to the the Edge list that's mapped to the "from" vertex
// shows what vertices are connected to the "from" vertex and those edge weights, in case of multiple edges from a single vertex
func (g *Graph) AddEdge(to, from Pair, w *float32) {
	g.Edges[from] = append(g.Edges[from], Edge{Destination: to, Weight: w})
}

type Colours struct {
	colorList []*float32
}

// a combination of the assignment instructions and the OpenGL code from Conway's to determine if the cell should be drawable or not
type Cell struct {
	Drawable           uint32
	Nest, Food         bool
	IsAnt              bool
	IsHomePheromone    bool
	IsFoodPheromone    bool
	PheromoneDecay     float32
	PheromoneHomeLevel float32
	PheromoneFoodLevel float32
	PheromoneHomeTime  time.Time
	PheromoneFoodTime  time.Time
	PheromoneFade      []Colours
}

// checks the cell to determine if it contains a nest, food, pheromones, or ant
// draws the cell if it is, otherwise the cell is empty
func (c *Cell) Draw() {
	if !c.Nest && !c.Food && !c.IsAnt && !c.IsHomePheromone && !c.IsFoodPheromone {
		return
	}
	gl.BindVertexArray(c.Drawable)
	gl.DrawArrays(gl.TRIANGLES, 0, int32(len(Square)/3))
}

// this function updates the pheromone fade (sets the color of the trail for visual purposes)
func (c *Cell) PheromoneDraw() {
	if c.PheromoneFade == nil {
		c.PheromoneFade = make([]Colours, 2)
		c.PheromoneFade = append(c.PheromoneFade, Colours{})
		c.PheromoneFade = append(c.PheromoneFade, Colours{})
		c.PheromoneFade[0].colorList = make([]*float32, 3)
		c.PheromoneFade[1].colorList = make([]*float32, 3)
	}
	for i := range c.PheromoneFade[0].colorList {
		c.PheromoneFade[0].colorList[i] = new(float32)
		*c.PheromoneFade[0].colorList[i] = 1.0
		c.PheromoneFade[1].colorList[i] = new(float32)
	}
	*c.PheromoneFade[1].colorList[0] = 0.4
	*c.PheromoneFade[1].colorList[1] = 0.3
	*c.PheromoneFade[1].colorList[2] = 0.9
}

type Ant struct { // I found some things online for how to create an Ant, but ultimately decided to just make it my own way
	CurPos, LastPos   Pair
	PheromoneType     bool
	PheromoneStrength float32
	HomeBase          Pair
	HasFood           bool
	FoundFood         bool
	Direction         string
	Travel            Pair
}

// this function is called if the ant has not found food at all (a.HasFood == false && a.FoundFood == false)
func (a *Ant) NoFoodMove() {
	// randomize the direction of the ant's movement based on 8 cardinal directions (omni-directional movement)
	if a.Direction == "West" {
		a.Travel.X = rand.Intn(2) * (-1)
		a.Travel.Y = rand.Intn(3) - 1
	} else if a.Direction == "East" {
		a.Travel.X = rand.Intn(2)
		a.Travel.Y = rand.Intn(3) - 1
	} else if a.Direction == "North" {
		a.Travel.X = rand.Intn(3) - 1
		a.Travel.Y = rand.Intn(2)
	} else if a.Direction == "South" {
		a.Travel.X = rand.Intn(3) - 1
		a.Travel.Y = rand.Intn(2) * (-1)
	} else if a.Direction == "Southwest" {
		a.Travel.X = rand.Intn(2) * (-1)
		a.Travel.Y = rand.Intn(2) * (-1)
	} else if a.Direction == "Southeast" {
		a.Travel.X = rand.Intn(2)
		a.Travel.Y = rand.Intn(2) * (-1)
	} else if a.Direction == "Northwest" {
		a.Travel.X = rand.Intn(2) * (-1)
		a.Travel.Y = rand.Intn(2)
	} else if a.Direction == "Northeast" {
		a.Travel.X = rand.Intn(2)
		a.Travel.Y = rand.Intn(2)
	}

	// generate a random probability for the ant to change its cardinal direction
	a.Direction = a.GenerateCardinal() // further randomizes their direction of travel with 10% probability of a direction change or not
}

// this function generates a random probability for the ant to change its cardinal direction
func (a *Ant) GenerateCardinal() string {
	prob := rand.Float64()
	if prob <= 0.1 {
		return "West"
	} else if prob > 0.2 && prob <= 0.3 {
		return "East"
	} else if prob > 0.3 && prob <= 0.4 {
		return "North"
	} else if prob > 0.4 && prob <= 0.5 {
		return "South"
	} else if prob > 0.5 && prob <= 0.6 {
		return "Southwest"
	} else if prob > 0.6 && prob <= 0.7 {
		return "Southeast"
	} else if prob > 0.7 && prob <= 0.8 {
		return "Northwest"
	} else if prob > 0.8 && prob <= 0.9 {
		return "Northeast"
	} else {
		return a.Direction
	}
}

// this function handles the movement of the ant if it has found food or a food path, but does not have food itself (a.HasFood == false && a.FoundFood == true)
// handles the pathing of the ant to the food cluster by following the pheromone trail there
func (a *Ant) FoundFoodMove(cells [][]*Cell, pathFood *Graph, mut *sync.Mutex) {
	cells[a.LastPos.X][a.LastPos.Y].IsAnt = false // update the ant's position
	var highPair Pair
	pheromones := float32(0.0)

	mut.Lock() // since graph is not part of ant or cell object and is its own object being pointed to, must mut.Lock() to prevent concurrent read/write errors

	for _, edge := range pathFood.Edges[a.CurPos] { // tells the ant to choose the edge with the highest weight (strongest pheromones)
		if *edge.Weight > pheromones {
			pheromones = *edge.Weight
			highPair = edge.Destination
		}
	}

	mut.Unlock() // free up the graph to be read/written to by other ants

	// update the ant's position to be at the vertex associated with the highest edge weight (strongest pheromones)
	a.LastPos = a.CurPos
	a.CurPos = highPair
	cells[a.LastPos.X][a.LastPos.Y].IsAnt = true
}

// this function has the ant check if any cell nearby (within a 1-block radius) is food, if it is, it moves to it, else, it moves randomly (assuming a.FoundFood == false)
func (a *Ant) CheckFood(cells [][]*Cell) {
	if cells[a.CurPos.X][a.CurPos.Y].Food {
		a.HasFood = true
		a.FoundFood = true
		a.Travel = Pair{0, 0}
	} else if cells[(a.CurPos.X+1+Rows)%Rows][a.CurPos.Y].Food {
		a.HasFood = true
		a.FoundFood = true
		a.Travel = Pair{1, 0}
	} else if cells[(a.CurPos.X-1+Rows)%Rows][a.CurPos.Y].Food {
		a.HasFood = true
		a.FoundFood = true
		a.Travel = Pair{-1, 0}
	} else if cells[a.CurPos.X][(a.CurPos.Y+1+Cols)%Cols].Food {
		a.HasFood = true
		a.FoundFood = true
		a.Travel = Pair{0, 1}
	} else if cells[a.CurPos.X][(a.CurPos.Y-1+Cols)%Cols].Food {
		a.HasFood = true
		a.FoundFood = true
		a.Travel = Pair{0, -1}
	} else if cells[(a.CurPos.X+1+Rows)%Rows][(a.CurPos.Y+1+Cols)%Cols].Food {
		a.HasFood = true
		a.FoundFood = true
		a.Travel = Pair{1, 1}
	} else if cells[(a.CurPos.X+1+Rows)%Rows][(a.CurPos.Y-1+Cols)%Cols].Food {
		a.HasFood = true
		a.FoundFood = true
		a.Travel = Pair{1, -1}
	} else if cells[(a.CurPos.X-1+Rows)%Rows][(a.CurPos.Y+1+Cols)%Cols].Food {
		a.HasFood = true
		a.FoundFood = true
		a.Travel = Pair{-1, 1}
	} else if cells[(a.CurPos.X-1+Rows)%Rows][(a.CurPos.Y-1+Cols)%Cols].Food {
		a.HasFood = true
		a.FoundFood = true
		a.Travel = Pair{-1, -1}
	}
}

// this function handles the movement of the ants if the ant does not have food and food is not found
// it applies the random movement found by method NoFoodMove()
func (a *Ant) MoveHungryAnt(cells [][]*Cell, pathHome *Graph, mut *sync.Mutex) {
	cells[a.CurPos.X][a.CurPos.Y].IsAnt = false
	cells[a.CurPos.X][a.CurPos.Y].IsHomePheromone = true
	cells[a.CurPos.X][a.CurPos.Y].PheromoneDecay = Gamma
	cells[a.CurPos.X][a.CurPos.Y].PheromoneHomeLevel = a.PheromoneStrength
	cells[a.CurPos.X][a.CurPos.Y].PheromoneHomeTime = time.Now()
	a.PheromoneStrength = Alpha
	a.PheromoneType = false
	if cells[a.CurPos.X][a.CurPos.Y].IsFoodPheromone {
		a.FoundFood = true
	} else {
		a.LastPos = a.CurPos
		a.CurPos = Pair{(a.CurPos.X + a.Travel.X + Rows) % Rows, (a.CurPos.Y + a.Travel.Y + Cols) % Cols}
		mut.Lock()

		pathHome.AddVertex(a.LastPos)
		pathHome.AddVertex(a.CurPos)
		pathHome.AddEdge(a.LastPos, a.CurPos, &cells[a.CurPos.X][a.CurPos.Y].PheromoneHomeLevel)

		mut.Unlock()
		cells[a.CurPos.X][a.CurPos.Y].IsAnt = true
	}
}

// this function tells an ant with food (a.HasFood == true) to follow the strongest home pheromones back to the nest
func (a *Ant) BringFoodHome(cells [][]*Cell, pathHome *Graph, pathFood *Graph, count *int, mut *sync.Mutex) {
	a.PheromoneStrength = Beta
	a.PheromoneType = true
	a.FoundFood = true
	cells[a.CurPos.X][a.CurPos.Y].IsAnt = false
	cells[a.CurPos.X][a.CurPos.Y].IsFoodPheromone = true
	cells[a.CurPos.X][a.CurPos.Y].PheromoneDecay = Gamma / 3.0
	cells[a.CurPos.X][a.CurPos.Y].PheromoneFoodLevel = a.PheromoneStrength
	cells[a.CurPos.X][a.CurPos.Y].PheromoneFoodTime = time.Now()

	var highPair Pair
	pheromones := float32(0.0)

	mut.Lock()
	for _, edge := range pathHome.Edges[a.CurPos] {
		if *edge.Weight > pheromones {
			pheromones = *edge.Weight
			highPair = edge.Destination
		}
	}
	pathFood.AddVertex(a.CurPos)
	pathFood.AddVertex(highPair)
	pathFood.AddEdge(a.CurPos, highPair, &cells[highPair.X][highPair.Y].PheromoneFoodLevel)
	mut.Unlock()

	a.LastPos = a.CurPos
	a.CurPos = highPair

	if (a.CurPos.X == a.HomeBase.X && a.CurPos.Y == a.HomeBase.Y) || cells[a.CurPos.X][a.CurPos.Y].Nest {
		*count += 1
		log.Printf("Brought food home\nTotal Food at home: %d\n", *count)
		a.HasFood = false
	}
	cells[a.CurPos.X][a.CurPos.Y].IsAnt = true
}

// I got a simple movement function from copilot and added the logic to track the pheromone trail and update whether the cell
// contains an ant. This function no longer even remotely resembles what was given by copilot
func (a *Ant) Move(cells [][]*Cell, pathHome *Graph, pathFood *Graph, d time.Duration, count *int, wg *sync.WaitGroup, mut *sync.Mutex) {
	if (d%time.Duration(1000) == 0) && !a.FoundFood {
		a.NoFoodMove()
	} else if a.FoundFood && !a.HasFood {
		a.FoundFoodMove(cells, pathFood, mut)
	}

	cells[a.CurPos.X][a.CurPos.Y].PheromoneDraw()
	a.CheckFood(cells)

	if !a.HasFood {
		a.MoveHungryAnt(cells, pathHome, mut)
	} else if a.HasFood {
		a.BringFoodHome(cells, pathHome, pathFood, count, mut)
	}
	wg.Done()
}

// build a 3x3 square for the nest around the center nest spot
func BuildNest(cells [][]*Cell, spot []int) {
	cells[spot[0]][spot[1]].Nest = true
	cells[spot[0]][(spot[1]+Cols-1)%Cols].Nest = true
	cells[spot[0]][(spot[1]+Cols+1)%Cols].Nest = true
	cells[(spot[0]+Rows-1)%Rows][spot[1]].Nest = true
	cells[(spot[0]+Rows+1)%Rows][spot[1]].Nest = true
	cells[(spot[0]+Rows-1)%Rows][(spot[1]+Cols-1)%Cols].Nest = true
	cells[(spot[0]+Rows+1)%Rows][(spot[1]+Cols-1)%Cols].Nest = true
	cells[(spot[0]+Rows-1)%Rows][(spot[1]+Cols+1)%Cols].Nest = true
	cells[(spot[0]+Rows+1)%Rows][(spot[1]+Cols+1)%Cols].Nest = true

}

// spawns 12 ants around the nest edges and stores the ants location inside of the ant itself
// Can write a function called AntSpawner that takes the x,y location the ant will be spawned in
// and parallelize the spawning of the ants in that way
func SpawnAnts(cells [][]*Cell, spot []int) []*Ant {
	ants := make([]*Ant, 8)

	cells[spot[0]][(spot[1]+Cols-1)%Cols].IsAnt = true
	ants[0] = &Ant{
		PheromoneType:     false,
		PheromoneStrength: Alpha,
		HomeBase:          Pair{spot[0], (spot[1] + Cols - 1) % Cols},
		CurPos:            Pair{spot[0], (spot[1] + Cols - 1) % Cols},
		Direction:         "South",
	}

	cells[spot[0]][(spot[1]+Cols+1)%Cols].IsAnt = true
	ants[1] = &Ant{
		PheromoneType:     false,
		PheromoneStrength: Alpha,
		HomeBase:          Pair{spot[0], (spot[1] + Cols + 1) % Cols},
		CurPos:            Pair{spot[0], (spot[1] + Cols + 1) % Cols},
		Direction:         "North",
	}

	cells[(spot[0]+Rows-1)%Cols][spot[1]].IsAnt = true
	ants[2] = &Ant{
		PheromoneType:     false,
		PheromoneStrength: Alpha,
		HomeBase:          Pair{(spot[0] + Rows - 1) % Cols, spot[1]},
		CurPos:            Pair{(spot[0] + Rows - 1) % Cols, spot[1]},
		Direction:         "West",
	}

	cells[(spot[0]+Rows+1)%Cols][spot[1]].IsAnt = true
	ants[3] = &Ant{
		PheromoneType:     false,
		PheromoneStrength: Alpha,
		HomeBase:          Pair{(spot[0] + Rows + 1) % Cols, spot[1]},
		CurPos:            Pair{(spot[0] + Rows + 1) % Cols, spot[1]},
		Direction:         "East",
	}

	cells[(spot[0]+Rows-1)%Rows][(spot[1]+Cols-1)%Cols].IsAnt = true
	ants[4] = &Ant{
		PheromoneType:     false,
		PheromoneStrength: Alpha,
		HomeBase:          Pair{(spot[0] + Rows - 1) % Rows, (spot[1] + Cols - 1) % Cols},
		CurPos:            Pair{(spot[0] + Rows - 1) % Rows, (spot[1] + Cols - 1) % Cols},
		Direction:         "Southwest",
	}

	cells[(spot[0]+Rows+1)%Rows][(spot[1]+Cols-1)%Cols].IsAnt = true
	ants[5] = &Ant{
		PheromoneType:     false,
		PheromoneStrength: Alpha,
		HomeBase:          Pair{(spot[0] + Rows + 1) % Rows, (spot[1] + Cols - 1) % Cols},
		CurPos:            Pair{(spot[0] + Rows + 1) % Rows, (spot[1] + Cols - 1) % Cols},
		Direction:         "Southeast",
	}

	cells[(spot[0]+Rows-1)%Rows][(spot[1]+Cols+1)%Cols].IsAnt = true
	ants[6] = &Ant{
		PheromoneType:     false,
		PheromoneStrength: Alpha,
		HomeBase:          Pair{(spot[0] + Rows - 1) % Rows, (spot[1] + Cols + 1) % Cols},
		CurPos:            Pair{(spot[0] + Rows - 1) % Rows, (spot[1] + Cols + 1) % Cols},
		Direction:         "Northwest",
	}

	cells[(spot[0]+Rows+1)%Rows][(spot[1]+Cols+1)%Cols].IsAnt = true
	ants[7] = &Ant{
		PheromoneType:     false,
		PheromoneStrength: Alpha,
		HomeBase:          Pair{(spot[0] + Rows + 1) % Rows, (spot[1] + Cols + 1) % Cols},
		CurPos:            Pair{(spot[0] + Rows + 1) % Rows, (spot[1] + Cols + 1) % Cols},
		Direction:         "Northeast",
	}
	return ants
}

// spawns 3x3 cluster of food, food is infinite at this cluster
func SpawnFood(cells [][]*Cell, spot []int) {
	cells[spot[0]][spot[1]].Food = true
	cells[spot[0]][(spot[1]+Cols+1)%Cols].Food = true
	cells[spot[0]][(spot[1]+Cols-1)%Cols].Food = true
	cells[(spot[0]+Rows+1)%Rows][spot[1]].Food = true
	cells[(spot[0]+Rows-1)%Rows][spot[1]].Food = true
	cells[(spot[0]+Rows+1)%Rows][(spot[1]+Cols+1)%Cols].Food = true
	cells[(spot[0]+Rows+1)%Rows][(spot[1]+Cols-1)%Cols].Food = true
	cells[(spot[0]+Rows-1)%Rows][(spot[1]+Cols+1)%Cols].Food = true
	cells[(spot[0]+Rows-1)%Rows][(spot[1]+Cols-1)%Cols].Food = true
}

func MakeColony(count *int) ([][]*Cell, []*Ant) {
	nestSpot := []int{rand.Intn(Rows - 1), rand.Intn(Cols - 1)} // randomized the Nest spawn location
	// foodSpawn randomized the location of the food spawn as well as the amount
	foodSpawn := []int{rand.Intn(Rows - 1), rand.Intn(Cols - 1)}
	grid := make([][]*Cell, Cols) // make the cells
	for i := range Cols {
		for j := range Rows {
			c := newCell(i, j)
			grid[i] = append(grid[i], c) // populate the cells with the proper initialization values
		}
	}

	BuildNest(grid, nestSpot)         // this builds the nest in a random location
	ants := SpawnAnts(grid, nestSpot) // this spawns the ants around the nest
	SpawnFood(grid, foodSpawn)        // this spawns the food cluster in a random location

	log.Println(ants)

	return grid, ants
}

// initializes a new cell with the proper values. Function taken from Conway's and repurposed for use with the ants
func newCell(x, y int) *Cell {
	points := make([]float32, len(Square))
	copy(points, Square)

	for i := range points {
		var position, size float32
		switch i % 3 {
		case 0:
			size = 1.0 / float32(Cols)
			position = float32(x) * size
		case 1:
			size = 1.0 / float32(Rows)
			position = float32(y) * size
		default:
			continue
		}

		if points[i] < 0 {
			points[i] = (position * 2) - 1
		} else {
			points[i] = ((position + size) * 2) - 1
		}
	}

	return &Cell{
		Drawable:           makeVao(points),
		Nest:               false,
		Food:               false,
		IsAnt:              false,
		IsHomePheromone:    false,
		IsFoodPheromone:    false,
		PheromoneDecay:     Gamma,
		PheromoneHomeLevel: Alpha,
		PheromoneFoodLevel: 0,
		PheromoneFade:      nil,
	}
}

func main() {
	rand.New(rand.NewSource(time.Now().UnixNano())) // seed the random number generator                         // locks the main thread for rendering with OpenGL

	AntColours[0] = 1.0
	AntColours[1] = 0.1
	AntColours[2] = 0.1

	NestColours[0] = 0.9
	NestColours[1] = 0.1
	NestColours[2] = 0.7

	FoodColours[0] = 0.2
	FoodColours[1] = 0.9
	FoodColours[2] = 0.1

	window := initGlfw()   // initialize the window
	defer glfw.Terminate() // terminates the render window at the end of the main function

	program := initOpenGL() // create the shader for use with OpenGL

	homePath := &Graph{
		Vertices: []Vertex{},
		Edges:    make(map[Pair][]Edge),
	}

	foodPath := &Graph{
		Vertices: []Vertex{},
		Edges:    make(map[Pair][]Edge),
	}

	totalFood := new(int)

	cells, ants := MakeColony(totalFood) // create the grid with the colony and food cluster in it as well as a list of ants

	wg := new(sync.WaitGroup)
	mut := new(sync.Mutex)

	t := time.Duration(8) * time.Millisecond
	decay := time.Now()

	for !window.ShouldClose() {
		// log.Println("Inside the window")
		f := time.Now()

		for _, a := range ants { // traverse through list of ants
			wg.Add(1)
			go a.Move(cells, homePath, foodPath, time.Since(decay), totalFood, wg, mut) // move the ants in a random direction and update their position in the cells
		}

		wg.Wait()

		draw(cells, window, program, t) // draw the drawable cells

		time.Sleep(time.Second/time.Duration(Fps) - time.Since(f)) // lock framerate
	}
	runtime.UnlockOSThread()
}

// initGlfw initializes glfw and returns a Window object that can be used to render graphics.
func initGlfw() *glfw.Window {
	if err := glfw.Init(); err != nil {
		panic(err)
	}

	glfw.WindowHint(glfw.Resizable, glfw.False)
	glfw.WindowHint(glfw.ContextVersionMajor, 4)
	glfw.WindowHint(glfw.ContextVersionMinor, 6)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)

	window, err := glfw.CreateWindow(GridWidth, GridHeight, "Ant Colony Simulation", nil, nil)
	if err != nil {
		panic(err)
	}
	window.MakeContextCurrent()
	glfw.SwapInterval(glfw.True)

	return window
}

// initOpenGL initializes OpenGL and returns an initialized shader program
func initOpenGL() uint32 {
	if err := gl.Init(); err != nil {
		panic(err)
	}
	version := gl.GoStr(gl.GetString(gl.VERSION))
	log.Println("OpenGL version", version)

	vertexShader, err := compileShader(VertexShaderSource, gl.VERTEX_SHADER)
	if err != nil {
		panic(err)
	}
	fragmentShader, err := compileShader(FragmentShaderSource, gl.FRAGMENT_SHADER)
	if err != nil {
		panic(err)
	}

	prog := gl.CreateProgram()
	gl.AttachShader(prog, vertexShader)
	gl.AttachShader(prog, fragmentShader)
	gl.LinkProgram(prog)
	return prog
}

// draw clears anything that's on the screen before drawing new objects
// Cannot parallelize draws as OpenGL requires operations to happen on a single thread
func draw(cells [][]*Cell, window *glfw.Window, program uint32, d time.Duration) {
	gl.Clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)
	gl.UseProgram(program)
	vertexColorLocation := gl.GetUniformLocation(program, gl.Str("sprite_colour"+"\x00"))

	// https://learnopengl.com/Getting-started/Shaders for changing the color of cells using a single shader
	for x := range cells {
		for _, c := range cells[x] {
			// get the shader's uniform value
			if c.Nest {
				gl.Uniform4f(vertexColorLocation, NestColours[0], NestColours[1], NestColours[2], 1.0) // purple-ish for the nest
			}
			if c.Food {
				gl.Uniform4f(vertexColorLocation, FoodColours[0], FoodColours[1], FoodColours[2], 1.0) // green for the food
			}
			if c.IsHomePheromone && !c.IsFoodPheromone && !(c.Nest || c.Food || c.IsAnt) {
				decayPheromone(d, c, 0)
				gl.Uniform4f(vertexColorLocation, *c.PheromoneFade[0].colorList[0], *c.PheromoneFade[0].colorList[1], *c.PheromoneFade[0].colorList[2], 1.0)
			} else if c.IsFoodPheromone && !(c.Nest || c.Food || c.IsAnt) {
				decayPheromone(d, c, 1)
				gl.Uniform4f(vertexColorLocation, *c.PheromoneFade[1].colorList[0], *c.PheromoneFade[1].colorList[1], *c.PheromoneFade[1].colorList[2], 1.0)
			}
			if c.IsAnt {
				gl.Uniform4f(vertexColorLocation, AntColours[0], AntColours[1], AntColours[2], 1.0) // red for the ants
			}
			c.Draw()
		}
	}

	glfw.PollEvents()
	window.SwapBuffers()
}

func decayPheromone(d time.Duration, c *Cell, cdex int) {
	// log.Println(time.Since(c.PheromoneTime))
	if cdex == 0 {
		if time.Since(c.PheromoneHomeTime) > d {
			for i := range c.PheromoneFade[cdex].colorList {
				*c.PheromoneFade[cdex].colorList[i] -= c.PheromoneDecay
			}
		}
	} else {
		if time.Since(c.PheromoneFoodTime) > d {
			for i := range c.PheromoneFade[cdex].colorList {
				*c.PheromoneFade[cdex].colorList[i] -= c.PheromoneDecay
			}
		}
	}
}

// makeVao initializes and returns a vertex array from the points provided.
func makeVao(points []float32) uint32 {
	var vbo uint32
	gl.GenBuffers(2, &vbo)
	gl.BindBuffer(gl.ARRAY_BUFFER, vbo)
	gl.BufferData(gl.ARRAY_BUFFER, 4*len(points), gl.Ptr(points), gl.STATIC_DRAW)

	var vao uint32
	gl.GenVertexArrays(1, &vao)
	gl.BindVertexArray(vao)
	gl.EnableVertexAttribArray(0)
	gl.BindBuffer(gl.ARRAY_BUFFER, vbo)
	gl.VertexAttribPointer(0, 3, gl.FLOAT, false, 0, nil)

	return vao
}

// compileShader will send the shader source code to the GPU for compilation on the GPU (shaders handle vertex points of drawn objects as well as their color)
func compileShader(source string, shaderType uint32) (uint32, error) {
	shader := gl.CreateShader(shaderType)

	csources, free := gl.Strs(source)
	gl.ShaderSource(shader, 1, csources, nil)
	free()
	gl.CompileShader(shader)

	var status int32
	gl.GetShaderiv(shader, gl.COMPILE_STATUS, &status)
	if status == gl.FALSE {
		var logLength int32
		gl.GetShaderiv(shader, gl.INFO_LOG_LENGTH, &logLength)

		log := strings.Repeat("\x00", int(logLength+1))
		gl.GetShaderInfoLog(shader, logLength, nil, gl.Str(log))

		return 0, fmt.Errorf("failed to compile %v: %v", source, log)
	}

	return shader, nil
}
