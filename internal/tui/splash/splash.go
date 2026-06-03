package splash

import (
	"math"
	"math/rand"
	"time"

	tui "github.com/grindlemire/go-tui"

	"github.com/samcharles93/tau/internal/theme"
)

const (
	gridWidth        = 64
	gridHeight       = 26
	stormFrames      = 220
	cycles           = 1
	tickInterval     = 40
	initialAsteroids = 5
	maxAsteroids     = 28
	maxExplosions    = 32
)

type styleID int

const (
	styleEmpty styleID = iota
	styleStar
	styleGlyphCore
	styleGlyphMid
	styleGlyphSoft
	styleGlyphDim
	styleGlyphHeat
	styleGlyphHot
	styleTrail
	styleAsteroid
	styleParticle
)

type point struct {
	Column float64
	Row    float64
}

type screenCell struct {
	Character rune
	Style     styleID
	Priority  int
}

type tauCell struct {
	Column    int
	Row       int
	Character rune
	Layer     int
}

type star struct {
	Column       int
	Row          int
	Character    rune
	Phase        float64
	TwinkleSpeed float64
}

type trailPoint struct {
	Column float64
	Row    float64
	Age    int
}

type asteroid struct {
	Column       float64
	Row          float64
	VelocityCol  float64
	VelocityRow  float64
	Size         float64
	Character    rune
	WobblePhase  float64
	WobbleSpeed  float64
	WobbleAmount float64
	Trail        []trailPoint
	Active       bool
}

type ringPhase struct {
	RadiusOffset float64
	Speed        float64
	Amplitude    float64
	Frequency    float64
	Decay        float64
}

type impactParticle struct {
	Column      float64
	Row         float64
	VelocityCol float64
	VelocityRow float64
	Life        float64
	MaxLife     float64
	Character   rune
}

type explosion struct {
	Column     int
	Row        int
	Intensity  float64
	BirthFrame int
	Duration   int
	MaxRadius  float64
	Particles  []impactParticle
	Rings      []ringPhase
	Active     bool
}

type displacement struct {
	Column  float64
	Row     float64
	Shimmer float64
}

type sampledStroke struct {
	Polyline []point
	Radius   float64
}

type splashScreen struct {
	app               *tui.App
	frameTick         *tui.State[int]
	frame             int
	cycle             int
	tauCells          []tauCell
	tauLookup         map[[2]int]bool
	stars             []star
	asteroids         []asteroid
	explosions        []explosion
	nextAsteroidFrame int
	done              bool
}

// New returns a new splash screen component that runs a single cycle of the
// τ glyph asteroid-barrage animation, then exits.
func New() *splashScreen {
	splash := &splashScreen{
		frameTick: tui.NewState(0),
		cycle:     1,
	}
	splash.tauCells = buildTauShape()
	splash.tauLookup = make(map[[2]int]bool, len(splash.tauCells))
	for _, cell := range splash.tauCells {
		splash.tauLookup[[2]int{cell.Column, cell.Row}] = true
	}
	splash.resetScene()
	return splash
}

func (splash *splashScreen) BindApp(app *tui.App) {
	splash.app = app
	splash.frameTick.BindApp(app)
}

func (splash *splashScreen) Watchers() []tui.Watcher {
	return []tui.Watcher{
		tui.OnTimer(tickInterval*time.Millisecond, func() {
			splash.tick()
		}),
	}
}

func (splash *splashScreen) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.On(tui.KeyEscape, func(keyEvent tui.KeyEvent) { keyEvent.App().Stop() }),
		tui.On(tui.Rune('q'), func(keyEvent tui.KeyEvent) { keyEvent.App().Stop() }),
	}
}

func (splash *splashScreen) tick() {
	if splash.done {
		return
	}

	splash.frameTick.Set(splash.frameTick.Get() + 1)
	splash.frame++
	splash.updateScene()

	if splash.frame < stormFrames {
		return
	}

	if splash.cycle < cycles {
		splash.cycle++
		splash.frame = 0
		splash.resetScene()
		return
	}

	splash.done = true
	if splash.app != nil {
		splash.app.Stop()
	}
}

func (splash *splashScreen) resetScene() {
	splash.stars = buildStars(splash.tauLookup)
	splash.asteroids = splash.asteroids[:0]
	splash.explosions = splash.explosions[:0]
	splash.nextAsteroidFrame = 10 + rand.Intn(18)
	for asteroidIndex := 0; asteroidIndex < initialAsteroids; asteroidIndex++ {
		splash.asteroids = append(splash.asteroids, newAsteroid(true))
	}
}

func (splash *splashScreen) updateScene() {
	if splash.frame >= splash.nextAsteroidFrame && len(splash.asteroids) < maxAsteroids {
		splash.asteroids = append(splash.asteroids, newAsteroid(false))
		splash.nextAsteroidFrame = splash.frame + 10 + rand.Intn(24)
	}

	for asteroidIndex := range splash.asteroids {
		if !splash.asteroids[asteroidIndex].Active {
			splash.asteroids[asteroidIndex].ageTrail()
			continue
		}

		splash.asteroids[asteroidIndex].update(splash.frame)
		if hitCell, ok := splash.checkCollision(&splash.asteroids[asteroidIndex]); ok {
			intensity := 0.38 + splash.asteroids[asteroidIndex].Size*0.55
			splash.explosions = append(splash.explosions, newExplosion(hitCell.Column, hitCell.Row, intensity, splash.frame))
			if splash.asteroids[asteroidIndex].Size > 0.9 {
				fragmentCount := int(math.Round(splash.asteroids[asteroidIndex].Size * 1.6))
				for fragmentIndex := 0; fragmentIndex < fragmentCount; fragmentIndex++ {
					column := hitCell.Column + rand.Intn(5) - 2
					row := hitCell.Row + rand.Intn(5) - 2
					splash.explosions = append(splash.explosions, newExplosion(column, row, intensity*0.35, splash.frame))
				}
			}
		}
	}

	for explosionIndex := range splash.explosions {
		splash.explosions[explosionIndex].update(splash.frame)
	}

	splash.asteroids = filterAsteroids(splash.asteroids)
	splash.explosions = filterExplosions(splash.explosions)
	if len(splash.explosions) > maxExplosions {
		splash.explosions = splash.explosions[len(splash.explosions)-maxExplosions:]
	}
}

func (splash *splashScreen) checkCollision(currentAsteroid *asteroid) (tauCell, bool) {
	gridColumn := int(math.Round(currentAsteroid.Column))
	gridRow := int(math.Round(currentAsteroid.Row))
	hitThreshold := currentAsteroid.Size * 1.2

	for _, cell := range splash.tauCells {
		deltaColumn := float64(gridColumn - cell.Column)
		deltaRow := float64(gridRow - cell.Row)
		if math.Hypot(deltaColumn, deltaRow) < hitThreshold {
			currentAsteroid.Active = false
			return cell, true
		}
	}

	return tauCell{}, false
}

func (splash *splashScreen) Render(app *tui.App) *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithWidthPercent(100),
		tui.WithHeightPercent(100),
		tui.WithJustify(tui.JustifyCenter),
		tui.WithAlign(tui.AlignCenter),
	)

	canvas := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithWidth(gridWidth),
	)

	screen := splash.buildScreen()
	for row := 0; row < gridHeight; row++ {
		canvas.AddChild(renderRow(screen[row]))
	}

	root.AddChild(canvas)
	root.AddChild(tui.New(tui.WithText(" ")))

	if splash.frame > 28 {
		root.AddChild(tui.New(
			tui.WithText("tau"),
			tui.WithTextStyle(tui.NewStyle().Bold().Foreground(theme.ColorLilac)),
		))
	}

	if splash.frame > 58 {
		root.AddChild(tui.New(
			tui.WithText("agentic coding harness"),
			tui.WithTextStyle(theme.DimStyle()),
		))
	}

	return root
}

func (splash *splashScreen) buildScreen() [gridHeight][gridWidth]screenCell {
	var screen [gridHeight][gridWidth]screenCell
	for row := 0; row < gridHeight; row++ {
		for column := 0; column < gridWidth; column++ {
			screen[row][column] = screenCell{Character: ' ', Style: styleEmpty, Priority: 0}
		}
	}

	for _, backgroundStar := range splash.stars {
		twinkle := math.Sin(float64(splash.frame)*backgroundStar.TwinkleSpeed + backgroundStar.Phase)
		if twinkle > -0.55 {
			putCell(&screen, backgroundStar.Column, backgroundStar.Row, backgroundStar.Character, styleStar, 1)
		}
	}

	for _, cell := range splash.tauCells {
		totalColumnShift := 0.0
		totalRowShift := 0.0
		maxShimmer := 0.0

		for _, currentExplosion := range splash.explosions {
			if !currentExplosion.Active {
				continue
			}
			cellDisplacement := currentExplosion.getDisplacement(cell.Column, cell.Row, splash.frame)
			totalColumnShift += cellDisplacement.Column
			totalRowShift += cellDisplacement.Row
			maxShimmer = math.Max(maxShimmer, cellDisplacement.Shimmer)
		}

		displacementMagnitude := math.Hypot(totalColumnShift, totalRowShift)
		if displacementMagnitude > 0.95 {
			totalColumnShift *= 0.95 / displacementMagnitude
			totalRowShift *= 0.95 / displacementMagnitude
		}

		character := cell.Character
		cellStyle := styleForGlyphLayer(cell.Layer)
		priority := 3
		if maxShimmer > 0.62 {
			character = '▓'
			cellStyle = styleGlyphHot
			priority = 5
		} else if maxShimmer > 0.32 {
			character = '▒'
			cellStyle = styleGlyphHeat
			priority = 4
		}

		renderColumn := cell.Column + int(math.Round(totalColumnShift))
		renderRow := cell.Row + int(math.Round(totalRowShift))
		putCell(&screen, renderColumn, renderRow, character, cellStyle, priority)
	}

	for _, currentExplosion := range splash.explosions {
		for _, particle := range currentExplosion.Particles {
			if particle.Life <= 0 {
				continue
			}
			putCell(&screen, int(math.Round(particle.Column)), int(math.Round(particle.Row)), particle.Character, styleParticle, 7)
		}
	}

	for _, currentAsteroid := range splash.asteroids {
		for _, trail := range currentAsteroid.Trail {
			if trail.Age < 10 {
				putCell(&screen, int(math.Round(trail.Column)), int(math.Round(trail.Row)), '·', styleTrail, 6)
			}
		}
		if currentAsteroid.Active {
			putCell(&screen, int(math.Round(currentAsteroid.Column)), int(math.Round(currentAsteroid.Row)), currentAsteroid.Character, styleAsteroid, 8)
		}
	}

	return screen
}

func renderRow(cells [gridWidth]screenCell) *tui.Element {
	rowElement := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Row),
		tui.WithWidth(gridWidth),
	)

	column := 0
	for column < gridWidth {
		currentStyle := cells[column].Style
		startColumn := column
		for column < gridWidth && cells[column].Style == currentStyle {
			column++
		}

		segment := make([]rune, column-startColumn)
		for segmentIndex := range segment {
			segment[segmentIndex] = cells[startColumn+segmentIndex].Character
		}

		if currentStyle == styleEmpty {
			rowElement.AddChild(tui.New(tui.WithText(string(segment))))
			continue
		}
		rowElement.AddChild(tui.New(
			tui.WithText(string(segment)),
			tui.WithTextStyle(styleForID(currentStyle)),
		))
	}

	return rowElement
}

func putCell(screen *[gridHeight][gridWidth]screenCell, column int, row int, character rune, style styleID, priority int) {
	if column < 0 || column >= gridWidth || row < 0 || row >= gridHeight {
		return
	}
	if priority < screen[row][column].Priority {
		return
	}
	screen[row][column] = screenCell{Character: character, Style: style, Priority: priority}
}

func styleForGlyphLayer(layer int) styleID {
	switch layer {
	case 0:
		return styleGlyphCore
	case 1:
		return styleGlyphMid
	case 2:
		return styleGlyphSoft
	default:
		return styleGlyphDim
	}
}

func styleForID(style styleID) tui.Style {
	switch style {
	case styleStar:
		return tui.NewStyle().Foreground(theme.ColorLilacDim)
	case styleGlyphCore:
		return tui.NewStyle().Bold().Foreground(theme.ColorLilac)
	case styleGlyphMid:
		return tui.NewStyle().Bold().Foreground(theme.ColorLilacMid)
	case styleGlyphSoft:
		return tui.NewStyle().Foreground(theme.ColorLilacSoft)
	case styleGlyphDim:
		return tui.NewStyle().Foreground(theme.ColorLilacDim)
	case styleGlyphHeat:
		return tui.NewStyle().Bold().Foreground(theme.ColorCoralDim)
	case styleGlyphHot:
		return tui.NewStyle().Bold().Foreground(theme.ColorCoralDeep)
	case styleTrail:
		return tui.NewStyle().Foreground(theme.ColorCoralDim)
	case styleAsteroid:
		return tui.NewStyle().Bold().Foreground(theme.ColorCoral)
	case styleParticle:
		return tui.NewStyle().Bold().Foreground(theme.ColorCoralDeep)
	default:
		return tui.NewStyle()
	}
}

func buildStars(tauLookup map[[2]int]bool) []star {
	starCount := gridWidth * gridHeight / 34
	stars := make([]star, 0, starCount)
	characters := []rune{'·', '·', '·', '•', '⋆'}
	for starIndex := 0; starIndex < starCount; starIndex++ {
		column := rand.Intn(gridWidth)
		row := rand.Intn(gridHeight)
		if tauLookup[[2]int{column, row}] {
			continue
		}
		stars = append(stars, star{
			Column:       column,
			Row:          row,
			Character:    characters[rand.Intn(len(characters))],
			Phase:        rand.Float64() * 2 * math.Pi,
			TwinkleSpeed: 0.05 + rand.Float64()*0.06,
		})
	}
	return stars
}

func newAsteroid(startNearCenter bool) asteroid {
	characters := []rune{'●', '◉', '◎', '◆', '◇'}
	sizes := []float64{0.65, 0.75, 0.85, 1.0, 1.15}
	targetColumn := float64(gridWidth)/2 + (rand.Float64()-0.5)*float64(gridWidth)*0.28
	targetRow := float64(gridHeight)/2 + (rand.Float64()-0.5)*float64(gridHeight)*0.28
	startColumn, startRow := asteroidStartPosition()

	if startNearCenter {
		startColumn = float64(gridWidth)/2 + (rand.Float64()-0.5)*float64(gridWidth)*0.75
		startRow = float64(gridHeight)/2 + (rand.Float64()-0.5)*float64(gridHeight)*0.75
	}

	directionColumn := targetColumn - startColumn
	directionRow := targetRow - startRow
	distance := math.Hypot(directionColumn, directionRow)
	if distance < 0.001 {
		distance = 1
	}
	speed := 0.22 + rand.Float64()*0.32

	return asteroid{
		Column:       startColumn,
		Row:          startRow,
		VelocityCol:  directionColumn / distance * speed,
		VelocityRow:  directionRow / distance * speed,
		Size:         sizes[rand.Intn(len(sizes))],
		Character:    characters[rand.Intn(len(characters))],
		WobblePhase:  rand.Float64() * 2 * math.Pi,
		WobbleSpeed:  0.08 + rand.Float64()*0.12,
		WobbleAmount: 0.04 + rand.Float64()*0.08,
		Trail:        make([]trailPoint, 0, 8),
		Active:       true,
	}
}

func asteroidStartPosition() (float64, float64) {
	margin := 3.0
	side := rand.Intn(4)
	switch side {
	case 0:
		return margin + rand.Float64()*(float64(gridWidth)-margin*2), -margin
	case 1:
		return margin + rand.Float64()*(float64(gridWidth)-margin*2), float64(gridHeight) + margin
	case 2:
		return -margin, margin + rand.Float64()*(float64(gridHeight)-margin*2)
	default:
		return float64(gridWidth) + margin, margin + rand.Float64()*(float64(gridHeight)-margin*2)
	}
}

func (currentAsteroid *asteroid) update(frame int) {
	currentSpeed := math.Hypot(currentAsteroid.VelocityCol, currentAsteroid.VelocityRow)
	if currentSpeed > 0.001 {
		perpendicularColumn := -currentAsteroid.VelocityRow / currentSpeed
		perpendicularRow := currentAsteroid.VelocityCol / currentSpeed
		wobble := math.Sin(currentAsteroid.WobblePhase+float64(frame)*currentAsteroid.WobbleSpeed) * currentAsteroid.WobbleAmount
		currentAsteroid.Column += currentAsteroid.VelocityCol + perpendicularColumn*wobble
		currentAsteroid.Row += currentAsteroid.VelocityRow + perpendicularRow*wobble
	} else {
		currentAsteroid.Column += currentAsteroid.VelocityCol
		currentAsteroid.Row += currentAsteroid.VelocityRow
	}

	currentAsteroid.Trail = append(currentAsteroid.Trail, trailPoint{
		Column: currentAsteroid.Column,
		Row:    currentAsteroid.Row,
		Age:    0,
	})
	if len(currentAsteroid.Trail) > 8 {
		currentAsteroid.Trail = currentAsteroid.Trail[1:]
	}
	currentAsteroid.ageTrail()

	if currentAsteroid.Column < -8 || currentAsteroid.Column > float64(gridWidth)+8 || currentAsteroid.Row < -8 || currentAsteroid.Row > float64(gridHeight)+8 {
		currentAsteroid.Active = false
	}
}

func (currentAsteroid *asteroid) ageTrail() {
	for trailIndex := range currentAsteroid.Trail {
		currentAsteroid.Trail[trailIndex].Age++
	}
	keptTrail := currentAsteroid.Trail[:0]
	for _, trail := range currentAsteroid.Trail {
		if trail.Age < 10 {
			keptTrail = append(keptTrail, trail)
		}
	}
	currentAsteroid.Trail = keptTrail
}

func newExplosion(column int, row int, intensity float64, birthFrame int) explosion {
	intensity = clampFloat(intensity, 0.3, 1.0)
	particleCount := 10 + int(math.Round(intensity*18))
	particles := make([]impactParticle, 0, particleCount)
	particleChars := []rune{'✦', '✧', '*', '·', '•', '◇', '◆'}
	for particleIndex := 0; particleIndex < particleCount; particleIndex++ {
		angle := rand.Float64() * 2 * math.Pi
		speed := 0.12 + rand.Float64()*0.55*intensity
		life := 8 + rand.Float64()*20
		particles = append(particles, impactParticle{
			Column:      float64(column),
			Row:         float64(row),
			VelocityCol: math.Cos(angle) * speed,
			VelocityRow: math.Sin(angle) * speed,
			Life:        life,
			MaxLife:     life,
			Character:   particleChars[rand.Intn(len(particleChars))],
		})
	}

	ringCount := 3 + int(math.Round(intensity*4))
	rings := make([]ringPhase, 0, ringCount)
	maxRadius := 3 + intensity*8
	for ringIndex := 0; ringIndex < ringCount; ringIndex++ {
		rings = append(rings, ringPhase{
			RadiusOffset: float64(ringIndex) * (maxRadius / float64(ringCount)) * 0.45,
			Speed:        0.11 + rand.Float64()*0.17,
			Amplitude:    0.2 + rand.Float64()*0.5*intensity,
			Frequency:    1.2 + rand.Float64()*2.2,
			Decay:        0.58 + rand.Float64()*0.32,
		})
	}

	return explosion{
		Column:     column,
		Row:        row,
		Intensity:  intensity,
		BirthFrame: birthFrame,
		Duration:   36 + int(math.Round(intensity*30)),
		MaxRadius:  maxRadius,
		Particles:  particles,
		Rings:      rings,
		Active:     true,
	}
}

func (currentExplosion *explosion) update(frame int) {
	age := frame - currentExplosion.BirthFrame
	if age > currentExplosion.Duration {
		currentExplosion.Active = false
	}

	for particleIndex := range currentExplosion.Particles {
		currentExplosion.Particles[particleIndex].Column += currentExplosion.Particles[particleIndex].VelocityCol
		currentExplosion.Particles[particleIndex].Row += currentExplosion.Particles[particleIndex].VelocityRow
		currentExplosion.Particles[particleIndex].VelocityCol *= 0.92
		currentExplosion.Particles[particleIndex].VelocityRow *= 0.92
		currentExplosion.Particles[particleIndex].Life--
	}

	currentExplosion.Particles = filterParticles(currentExplosion.Particles)
}

func (currentExplosion explosion) getDisplacement(column int, row int, frame int) displacement {
	age := float64(frame - currentExplosion.BirthFrame)
	if age < 0 || age > float64(currentExplosion.Duration) {
		return displacement{}
	}

	deltaColumn := float64(column - currentExplosion.Column)
	deltaRow := float64(row - currentExplosion.Row)
	distance := math.Hypot(deltaColumn, deltaRow)
	normalColumn := 1.0
	normalRow := 0.0
	if distance > 0.001 {
		normalColumn = deltaColumn / distance
		normalRow = deltaRow / distance
	}

	globalDecay := math.Exp(-age / (float64(currentExplosion.Duration) * 0.5))
	spatialDecay := math.Exp(-distance / (currentExplosion.MaxRadius * 0.72))
	totalDisplacement := 0.0

	for _, ring := range currentExplosion.Rings {
		ringRadius := ring.RadiusOffset + age*ring.Speed
		ringDistance := distance - ringRadius
		ringWidth := 0.75 + ring.Amplitude*0.45
		ringFactor := math.Exp(-(ringDistance * ringDistance) / (ringWidth * ringWidth))
		totalDisplacement += math.Sin(distance*ring.Frequency-age*0.34) * ring.Amplitude * ringFactor * ring.Decay
	}

	totalDisplacement *= globalDecay * spatialDecay * currentExplosion.Intensity * 0.36
	maxDisplacement := 0.9 * currentExplosion.Intensity
	totalDisplacement = clampFloat(totalDisplacement, -maxDisplacement, maxDisplacement)
	shimmer := 0.0
	if math.Abs(totalDisplacement) > 0.035 && maxDisplacement > 0 {
		shimmer = math.Abs(totalDisplacement) / maxDisplacement
	}

	return displacement{
		Column:  normalColumn * totalDisplacement,
		Row:     normalRow * totalDisplacement,
		Shimmer: shimmer,
	}
}

func filterAsteroids(asteroids []asteroid) []asteroid {
	keptAsteroids := asteroids[:0]
	for _, currentAsteroid := range asteroids {
		if currentAsteroid.Active || len(currentAsteroid.Trail) > 0 {
			keptAsteroids = append(keptAsteroids, currentAsteroid)
		}
	}
	return keptAsteroids
}

func filterExplosions(explosions []explosion) []explosion {
	keptExplosions := explosions[:0]
	for _, currentExplosion := range explosions {
		if currentExplosion.Active || len(currentExplosion.Particles) > 0 {
			keptExplosions = append(keptExplosions, currentExplosion)
		}
	}
	return keptExplosions
}

func filterParticles(particles []impactParticle) []impactParticle {
	keptParticles := particles[:0]
	for _, particle := range particles {
		if particle.Life > 0 {
			keptParticles = append(keptParticles, particle)
		}
	}
	return keptParticles
}

func buildTauShape() []tauCell {
	centerColumn := float64(gridWidth) / 2
	centerRow := float64(gridHeight)/2 + 1
	columnScale := float64(gridWidth) * 0.34
	rowScale := float64(gridHeight) * 0.42

	mapPoint := func(column float64, row float64) point {
		return point{
			Column: centerColumn + column*columnScale,
			Row:    centerRow + row*rowScale,
		}
	}

	strokes := []sampledStroke{
		{
			Polyline: sampleCurve([]point{mapPoint(-0.72, -0.34), mapPoint(-0.43, -0.47), mapPoint(-0.05, -0.44), mapPoint(0.45, -0.34), mapPoint(0.70, -0.26)}),
			Radius:   1.45,
		},
		{
			Polyline: sampleCurve([]point{mapPoint(-0.70, -0.33), mapPoint(-0.78, -0.08), mapPoint(-0.65, 0.12), mapPoint(-0.42, 0.17)}),
			Radius:   1.12,
		},
		{
			Polyline: sampleCurve([]point{mapPoint(0.31, -0.34), mapPoint(0.25, -0.02), mapPoint(0.22, 0.39), mapPoint(0.11, 0.74), mapPoint(-0.06, 0.86)}),
			Radius:   1.34,
		},
		{
			Polyline: sampleCurve([]point{mapPoint(0.02, 0.82), mapPoint(-0.17, 0.87), mapPoint(-0.34, 0.76)}),
			Radius:   0.95,
		},
	}

	seen := make(map[[2]int]bool)
	cells := make([]tauCell, 0, 180)
	for row := 0; row < gridHeight; row++ {
		for column := 0; column < gridWidth; column++ {
			cellPoint := point{Column: float64(column), Row: float64(row)}
			bestDistance := math.MaxFloat64
			bestRadius := 1.0
			for _, stroke := range strokes {
				distance := distanceToPolyline(cellPoint, stroke.Polyline)
				if distance < bestDistance {
					bestDistance = distance
					bestRadius = stroke.Radius
				}
			}

			if bestDistance > bestRadius+0.62 {
				continue
			}

			key := [2]int{column, row}
			if seen[key] {
				continue
			}
			seen[key] = true

			layer := 3
			character := '░'
			switch {
			case bestDistance <= bestRadius*0.44:
				layer = 0
				character = '█'
			case bestDistance <= bestRadius*0.68:
				layer = 1
				character = '▓'
			case bestDistance <= bestRadius*0.96:
				layer = 2
				character = '▒'
			}

			cells = append(cells, tauCell{
				Column:    column,
				Row:       row,
				Character: character,
				Layer:     layer,
			})
		}
	}

	return cells
}

func sampleCurve(controlPoints []point) []point {
	if len(controlPoints) < 2 {
		return controlPoints
	}

	polyline := make([]point, 0, len(controlPoints)*16)
	for pointIndex := 0; pointIndex < len(controlPoints)-1; pointIndex++ {
		previousPoint := controlPoints[maxInt(0, pointIndex-1)]
		currentPoint := controlPoints[pointIndex]
		nextPoint := controlPoints[pointIndex+1]
		afterNextPoint := controlPoints[minInt(len(controlPoints)-1, pointIndex+2)]
		for step := 0; step < 16; step++ {
			curveTime := float64(step) / 16.0
			polyline = append(polyline, catmullRom(previousPoint, currentPoint, nextPoint, afterNextPoint, curveTime))
		}
	}
	polyline = append(polyline, controlPoints[len(controlPoints)-1])
	return polyline
}

func catmullRom(first point, second point, third point, fourth point, curveTime float64) point {
	timeSquared := curveTime * curveTime
	timeCubed := timeSquared * curveTime
	return point{
		Column: 0.5 * ((2 * second.Column) + (-first.Column+third.Column)*curveTime + (2*first.Column-5*second.Column+4*third.Column-fourth.Column)*timeSquared + (-first.Column+3*second.Column-3*third.Column+fourth.Column)*timeCubed),
		Row:    0.5 * ((2 * second.Row) + (-first.Row+third.Row)*curveTime + (2*first.Row-5*second.Row+4*third.Row-fourth.Row)*timeSquared + (-first.Row+3*second.Row-3*third.Row+fourth.Row)*timeCubed),
	}
}

func distanceToPolyline(cellPoint point, polyline []point) float64 {
	bestDistance := math.MaxFloat64
	for pointIndex := 0; pointIndex < len(polyline)-1; pointIndex++ {
		distance := distanceToSegment(cellPoint, polyline[pointIndex], polyline[pointIndex+1])
		if distance < bestDistance {
			bestDistance = distance
		}
	}
	return bestDistance
}

func distanceToSegment(cellPoint point, start point, end point) float64 {
	deltaColumn := end.Column - start.Column
	deltaRow := end.Row - start.Row
	lengthSquared := deltaColumn*deltaColumn + deltaRow*deltaRow
	if lengthSquared <= 0.0001 {
		return math.Hypot(cellPoint.Column-start.Column, cellPoint.Row-start.Row)
	}
	segmentTime := ((cellPoint.Column-start.Column)*deltaColumn + (cellPoint.Row-start.Row)*deltaRow) / lengthSquared
	segmentTime = clampFloat(segmentTime, 0, 1)
	nearestColumn := start.Column + segmentTime*deltaColumn
	nearestRow := start.Row + segmentTime*deltaRow
	return math.Hypot(cellPoint.Column-nearestColumn, cellPoint.Row-nearestRow)
}

func clampFloat(value float64, lower float64, upper float64) float64 {
	if value < lower {
		return lower
	}
	if value > upper {
		return upper
	}
	return value
}

func minInt(first int, second int) int {
	if first < second {
		return first
	}
	return second
}

func maxInt(first int, second int) int {
	if first > second {
		return first
	}
	return second
}
