// Package ldtkgo is a loader for LDtk projects, written in Golang. The general idea is to load a project using ldtkgo.LoadFile() or ldtkgo.LoadBytes(), and then use the resulting Project.
// Generally, the smoothest way to use this in game development seems to be to render the layers out to images, and then draw them onscreen with a rendering or game development
// framework, like Pixel, raylib-go, or ebiten. All of the major elements of LDtk should be supported, including Levels, Layers, Tiles, AutoLayers, IntGrids, Entities, and Properties.
package ldtkgo

import (
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"io"
	"io/fs"
	"path/filepath"
	"strconv"

	"github.com/tidwall/gjson"
)

// LayerType constants indicating a Layer's type.
const (
	LayerTypeIntGrid  = "IntGrid"
	LayerTypeAutoTile = "AutoLayer"
	LayerTypeTile     = "Tiles"
	LayerTypeEntity   = "Entities"
)

// WorldLayout constants indicating direction or layout system for Worlds.
const (
	WorldLayoutHorizontal = "LinearHorizontal"
	WorldLayoutVertical   = "LinearVertical"
	WorldLayoutFree       = "Free"
	WorldLayoutGridVania  = "GridVania"
)

// Property represents custom Properties created and customized on Entities.
type Property struct {
	Identifier string      `json:"__identifier"`
	Type       string      `json:"__type"`  // The Type of the Property.
	Value      interface{} `json:"__value"` // The value contained within the property.
	project    *Project    `json:"-"`
}

// AsInt returns a property's value as an int. Note that this function doesn't check to ensure the value is the specified type before returning it.
func (p *Property) AsInt() int {
	return int(p.AsFloat64())
}

// AsFloat64 returns a property's value as a float64. Note that this function doesn't check to ensure the value is the specified type before returning it.
func (p *Property) AsFloat64() float64 {
	return p.Value.(float64)
}

// AsString returns a property's value as a string. Can be used for strings, colors, enums, etc. Note that this function doesn't check to ensure the value is the specified type before returning it.
func (p *Property) AsString() string {
	return p.Value.(string)
}

// AsBool returns a property's value as a boolean value. Note that this function doesn't check to ensure the value is the specified type before returning it.
func (p *Property) AsBool() bool {
	return p.Value.(bool)
}

// AsArray returns a property's value as an array of interface{} values. Note that this function doesn't check to ensure the value is the specified type before returning it.
func (p *Property) AsArray() []interface{} {
	return p.Value.([]interface{})
}

// AsMap returns a property's value as a map of string to interface{} values. As an aside, the JSON deserialization process turns LDtk Points into Maps, where the key is "cx" or
// "cy", and the value is the x and y position as float64s. Note that this function doesn't check to ensure the value is the specified type before returning it.
func (p *Property) AsMap() map[string]interface{} {
	return p.Value.(map[string]interface{})
}

// AsEntityRef returns a proprety's value as an Entity reference.
// Note that this function doesn't check to ensure the value is the specified type before returning it.
func (p *Property) AsEntityRef() *Entity {
	ref := p.AsMap()
	return p.project.LevelByIID(ref["levelIid"].(string)).LayerByIID(ref["layerIid"].(string)).EntityByIID(ref["entityIid"].(string))
}

func (p *Property) IsNull() bool {
	return p.Value == nil
}

// AsColor returns a property's value as a color.Color struct. Note that this function doesn't check to ensure the value is the specified type before returning it.
func (p *Property) AsColor() color.Color {
	color, _ := parseHexColorFast(p.AsString())
	return color
}

// TileRect represents the rectangle from which an Entity tile is
type TileRect struct {
	X          int `json:"x`
	Y          int `json:"y`
	W          int `json:"w`
	H          int `json:"h`
	TilesetUID int `json:"tilesetUid"`
	Tileset    *Tileset
}

// An Entity represents an Entitydefintion as defined in the entities.
type EntityDefinition struct {
	Identifier string    `json:"identifier"` // Name of the Entity
	UID        int       `json:"uid"`        // IID of the Entity
	Width      int       `json:"width"`      // Width  of the Entity in pixels
	Height     int       `json:"height"`     // Height of the Entity in pixels
	Tags       []string  `json:"tags"`       // Tags (categories) assigned to the Entity
	TileRect   *TileRect `json:"tileRect"`
	PivotX     float32   `json:"pivotX"`
	PivotY     float32   `json:"pivotY"`
}

// An Entity represents an Entity as placed in the LDtk level.
type Entity struct {
	Identifier string      `json:"__identifier"`   // Name of the Entity
	IID        string      `json:"iid"`            // IID of the Entity
	Position   []int       `json:"px"`             // Position of the Entity (x, y)
	Width      int         `json:"width"`          // Width  of the Entity in pixels
	Height     int         `json:"height"`         // Height of the Entity in pixels
	Properties []*Property `json:"fieldInstances"` // The Properties defined on the Entity
	Pivot      []float32   `json:"__pivot"`        // Pivot position of the Entity (a centered Pivot would be 0.5, 0.5)
	Tags       []string    `json:"__tags"`         // Tags (categories) assigned to the Entity
	TileRect   *TileRect   `json:"__tile"`
	Data       interface{} `json:"-"` // Data allows you to attach key custom data to the entity post-parsing
	level      *Level      `json:"-"`
}

// WorldX returns the X position of the Entity in world space, adding in the positioning of the Level.
func (entity *Entity) WorldX() int {
	return entity.Position[0] + entity.level.WorldX
}

// WorldY returns the Y position of the Entity in world space, adding in the positioning of the Level.
func (entity *Entity) WorldY() int {
	return entity.Position[1] + entity.level.WorldY
}

// PropertyByIdentifier returns a Property by its Identifier string (name).
func (entity *Entity) PropertyByIdentifier(id string) *Property {

	for _, p := range entity.Properties {
		if p.Identifier == id {
			return p
		}
	}
	return nil

}

// Integer indicates the value for an individual "Integer Object" on the IntGrid layer.
type Integer struct {
	Position []int `json:"-"`       // Not actually available from the LDtk file, but added in afterwards as a convenience; the position of the Integer in pixels.
	Value    int   `json:"v"`       // The value of the Integer.
	ID       int   `json:"coordID"` // The ID of the Integer on the IntGrid.
}

// EnumSet represents a set of Enums applied to tiles; this is just for convenience so you can see if a Tile contains an enum easily.
type EnumSet []string

func (e EnumSet) Contains(enum string) bool {
	for _, v := range e {
		if v == enum {
			return true
		}
	}
	return false
}

// Tile represents a graphical tile (whether automatic or manually placed).
type Tile struct {
	Position []int `json:"px"` // Position of the Tile in pixels (x, y)
	Src      []int // The source position on the texture to draw this texture
	Flip     byte  `json:"f"` // Flip bits - first bit is for X-flip, second is for Y. 0 = no flip, 1 = horizontal flip, 2 = vertical flip, 3 = both flipped
	ID       int   `json:"t"` // The ID of the Tile (starting from 0).
}

// FlipX returns if the Tile is flipped horizontally.
func (t *Tile) FlipX() bool {
	return t.Flip&1 > 0
}

// FlipY returns if the Tile is flipped vertically.
func (t *Tile) FlipY() bool {
	return t.Flip&2 > 0
}

// Layer represents a Layer, which can be of multiple types (Entity, AutoTile, Tile, or IntGrid).
type Layer struct {
	// The width and height of the layer
	Identifier string   `json:"__identifier"`     // Identifier (name) of the Layer
	IID        string   `json:"iid"`              // IID of the layer
	GridSize   int      `json:"__gridsize"`       // Grid size of the Layer
	OffsetX    int      `json:"__pxTotalOffsetX"` // The offset of the layer
	OffsetY    int      `json:"__pxTotalOffsetY"`
	CellWidth  int      `json:"__cWid"` // Overall width of the layer in cell count (i.e. a 160x80 level with 16x16 tiles would have a CellWidth and CellHeight of 10x5)
	CellHeight int      `json:"__cHei"` // Overall height of the layer in cell count
	Type       string   `json:"__type"` // Type of Layer. Can be compared using LayerType constants
	Tileset    *Tileset `json:"-"`      // Reference to the Tileset used for this Layer (assuming the path is the same)
	// TilesetPath string     `json:"__tilesetRelPath"` // Relative path to the tileset image; already is normalized using filepath.FromSlash().
	TilesetUID int        `json:"__tilesetDefUid"` // The UID of the used tileset
	IntGrid    []*Integer `json:"-"`
	AutoTiles  []*Tile    `json:"autoLayerTiles"` // Automatically set if IntGrid has values
	Tiles      []*Tile    `json:"gridTiles"`
	Entities   []*Entity  `json:"entityInstances"`
	Visible    bool       `json:"visible"` // Whether the layer is visible in LDtk
	level      *Level     `json:"-"`
}

// ForEachTile runs a callback for each tile in the Layer. This is to make it simpler to run a render loop regardless of if the Layer is composed of auto tiles or
// manually placed tiles.
func (layer *Layer) ForEachTile(function func(tile *Tile)) {
	for _, tile := range layer.Tiles {
		function(tile)
	}
	for _, tile := range layer.AutoTiles {
		function(tile)
	}
}

// EntityByIdentifier returns the Entity with the identifier (name) specified. If no Entity with the name is found, the function returns nil.
func (layer *Layer) EntityByIdentifier(identifier string) *Entity {
	for _, entity := range layer.Entities {
		if entity.Identifier == identifier {
			return entity
		}
	}
	return nil
}

// EntityByIID returns the Entity with the IID specified. If no Entity with the name is found, the function returns nil.
func (layer *Layer) EntityByIID(iid string) *Entity {
	for _, entity := range layer.Entities {
		if entity.IID == iid {
			return entity
		}
	}
	return nil
}

// ToGridPosition converts the specified position from a position in world space to a position on the Layer's grid. For example, if the layer were 128x128 and had 16x16 tiles, ToGridPosition(32, 16) would return (2, 1).
func (layer *Layer) ToGridPosition(x, y int) (int, int) {
	x /= layer.GridSize
	y /= layer.GridSize
	return x, y
}

// FromGridPosition converts the specified position from a position on the Layer's grid to world space. For example, if the layer were 128x128 and had 16x16 tiles, FromGridPosition(3, 4) would return (48, 64).
func (layer *Layer) FromGridPosition(x, y int) (int, int) {
	x *= layer.GridSize
	y *= layer.GridSize
	return x, y
}

// TileAt returns the Tile at the specified grid (not world) X and Y position.
// Note that this doesn't take into account the Layer's local Offset values (so a tile at 3, 4
// on a layer with an offset of 64, 64 would still be found at 3, 4).
func (layer *Layer) TileAt(x, y int) *Tile {

	for _, tile := range layer.Tiles {
		cx, cy := layer.ToGridPosition(tile.Position[0], tile.Position[1])
		if cx == x && cy == y {
			return tile
		}
	}

	return nil

}

// AutoTileAt returns the AutoLayer Tile at the specified grid (not world) X and Y position.
// Note that this doesn't take into account the Layer's local Offset values (so a tile at 3, 4 on a layer
// with an offset of 64, 64 would still be found at 3, 4).
func (layer *Layer) AutoTileAt(x, y int) *Tile {

	for _, autoTile := range layer.AutoTiles {
		cx, cy := layer.ToGridPosition(autoTile.Position[0], autoTile.Position[1])
		if cx == x && cy == y {
			return autoTile
		}
	}

	return nil

}

// IntegerAt returns the IntGrid Integer at the specified world X and Y position (rounded down to the Layer's grid).
// Note that this doesn't take into account the Layer's local Offset values (so a tile at 3, 4 on a layer with an
// offset of 64, 64 would still be found at 3, 4).
func (layer *Layer) IntegerAt(x, y int) *Integer {

	for _, integer := range layer.IntGrid {
		cx, cy := layer.ToGridPosition(integer.Position[0], integer.Position[1])
		if cx == x && cy == y {
			return integer
		}
	}

	return nil

}

// Index returns the index of the layer in the Level's layer stack.
func (layer *Layer) Index() int {
	for i, l := range layer.level.Layers {
		if l == layer {
			return i
		}
	}
	return -1
}

type Tileset struct {
	Path       string `json:"relPath"` // Relative path to the tileset image; already is normalized using filepath.FromSlash().
	ID         int    `json:"uid"`
	GridSize   int    `json:"tileGridSize"`
	Spacing    int
	Padding    int
	Width      int `json:"pxWid"`
	Height     int `json:"pxHei"`
	Identifier string
	CustomData map[int]string  `json:"-"` // Key: tileID, Value: custom data string
	Enums      map[int]EnumSet `json:"-"` // Key: enumValueID, Value: tileIDs (tile indices)
}

// CustomDataForTile returns the custom data defined for the tile of the ID given in the tileset. If no custom data is defined, a blank string is returned.
func (t *Tileset) CustomDataForTile(tileID int) string {
	if data, exists := t.CustomData[tileID]; exists {
		return data
	}
	return ""
}

// EnumsForTile returns the EnumSet defined for the tile of the ID given in the tileset. If no enums are defined, an empty EnumSet is returned.
func (t *Tileset) EnumsForTile(tileID int) EnumSet {
	if data, exists := t.Enums[tileID]; exists {
		return data
	}
	return EnumSet{}
}

// BGImage represents a Level's background image as definied withing LDtk (the filepath, the scale, etc).
type BGImage struct {
	Path     string
	ScaleX   float64
	ScaleY   float64
	CropRect []float64
}

// Level represents a Level in an LDtk Project.
type Level struct {
	Identifier    string // Name of the Level (i.e. "Level0")
	WorldX        int    // Position of the Level in the LDtk Project / world
	WorldY        int
	Width         int         `json:"pxWid"` // Width and height of the level in pixels.
	Height        int         `json:"pxHei"`
	IID           string      `json:"iid"` // IID of the level
	BGColorString string      `json:"__bgColor"`
	BGColor       color.Color `json:"-"`              // Background Color for the Level; will automatically default to the Project's if it is left at default in the LDtk project.
	Layers        []*Layer    `json:"layerInstances"` // The layers in the level in the project. Note that layers here (first is "furthest" / at the bottom, last is on top) is reversed compared to LDtk (first is at the top, bottom is on the bottom).
	Properties    []*Property `json:"fieldInstances"` // The Properties defined on the Entity
	BGImage       *BGImage    `json:"-"`              // Any background image that might be applied to this Level.
	Project       *Project    `json:"-"`
}

// LayerByIdentifier returns a Layer by its identifier (name). Returns nil if the specified Layer isn't found.
func (level *Level) LayerByIdentifier(identifier string) *Layer {
	for _, layer := range level.Layers {
		if layer.Identifier == identifier {
			return layer
		}
	}
	return nil
}

// LayerByIdentifier returns a Layer by its unique identifier. Returns nil if the specified Layer isn't found.
func (level *Level) LayerByIID(iid string) *Layer {
	for _, layer := range level.Layers {
		if layer.IID == iid {
			return layer
		}
	}
	return nil
}

// PropertyByIdentifier returns a Property by its Identifier string (name).
func (level *Level) PropertyByIdentifier(id string) *Property {

	for _, p := range level.Properties {
		if p.Identifier == id {
			return p
		}
	}
	return nil

}

// Project represents a full LDtk Project, allowing you access to the Levels within as well as some project-level properties.
type Project struct {
	WorldLayout       string
	WorldGridWidth    int
	WorldGridHeight   int
	BGColorString     string      `json:"defaultLevelBgColor"`
	BGColor           color.Color `json:"-"`
	JSONVersion       string
	Levels            []*Level
	Tilesets          []*Tileset
	IntGridNames      []string
	EntityDefinitions []*EntityDefinition
	// JSONData    string
}

// LevelByPosition returns the level that "contains" the point indicated by the X and Y values given, or nil if one isn't found.
// (Note that the world position is displayed in LDTK at the bottom in the status bar.)
func (project *Project) LevelByPosition(x, y int) *Level {

	for _, level := range project.Levels {

		rect := image.Rect(level.WorldX, level.WorldY, level.WorldX+level.Width, level.WorldY+level.Height)

		if rect.Min.X <= x && rect.Min.Y <= y && rect.Max.X >= x && rect.Max.Y >= y {
			return level
		}

	}

	return nil

}

// LevelByIdentifier returns the level that has the identifier specified, or nil if one isn't found.
func (project *Project) LevelByIdentifier(identifier string) *Level {
	for _, level := range project.Levels {
		if level.Identifier == identifier {
			return level
		}
	}
	return nil
}

// LevelByIID returns the level that has the unique identifier specified, or nil if one isn't found.
func (project *Project) LevelByIID(iid string) *Level {
	for _, level := range project.Levels {
		if level.IID == iid {
			return level
		}
	}
	return nil
}

func (project *Project) TilesetByIdentifier(identifier string) *Tileset {
	for _, tileset := range project.Tilesets {
		if tileset.Identifier == identifier {
			return tileset
		}
	}
	return nil
}

// EntityByIID returns the Entity by unique identifier specified, or nil if entity isn't found
func (project *Project) EntityByIID(iid string) *Entity {
	for _, level := range project.Levels {
		for _, layer := range level.Layers {
			for _, entity := range layer.Entities {
				if entity.IID == iid {
					return entity
				}
			}
		}
	}
	return nil
}

// EntityDefinitionByIdentifier returns the EntityDefinition by unique identifier specified, or nil if entity isn't found
func (project *Project) EntityDefinitionByIdentifier(identifier string) *EntityDefinition {
	for _, definition := range project.EntityDefinitions {
		if definition.Identifier == identifier {
			return definition
		}
	}
	return nil
}

// Open loads the LDtk project from the filepath specified using the file system provided.
// Open returns the Project and an error should the loading process fail (unable to find the file, unable to deserialize the JSON, etc).
func Open(filepath string, fileSystem fs.FS) (*Project, error) {

	file, err := fileSystem.Open(filepath)

	if err != nil {
		return nil, err
	}

	bytes, err := io.ReadAll(file)

	if err != nil {
		return nil, err
	}

	project, err := Read(bytes)

	return project, err

}

// Read reads the LDtk project using the specified slice of bytes. Returns the Project and an error should there be an error in the loading process (unable to properly deserialize the JSON).
func Read(data []byte) (*Project, error) {

	project := &Project{IntGridNames: []string{}}

	err := json.Unmarshal(data, project)

	if err != nil {
		return nil, err
	}

	dataStr := string(data)

	// Additional convenience fields

	if project.BGColorString != "" {
		project.BGColor, _ = parseHexColorFast(project.BGColorString)
	} else {
		project.BGColor = color.RGBA{}
	}

	tilesetByUID := map[int]*Tileset{}

	for _, tilesetDef := range gjson.Get(dataStr, `defs.tilesets`).Array() {

		newTS := &Tileset{CustomData: map[int]string{}, Enums: map[int]EnumSet{}}
		json.Unmarshal([]byte(tilesetDef.Raw), newTS)
		newTS.Path = filepath.FromSlash(newTS.Path)
		project.Tilesets = append(project.Tilesets, newTS)

		ts := project.TilesetByIdentifier(tilesetDef.Get("identifier").String())
		for _, enumSet := range tilesetDef.Get("enumTags").Array() {
			enumName := enumSet.Get("enumValueId").String()
			enumTiles := enumSet.Get("tileIds").Array()
			for _, idNumber := range enumTiles {
				id := int(idNumber.Int())
				if _, exists := ts.Enums[id]; !exists {
					ts.Enums[id] = EnumSet{}
				}
				ts.Enums[id] = append(ts.Enums[id], enumName)
			}
		}

		for _, customData := range tilesetDef.Get("customData").Array() {
			newTS.CustomData[int(customData.Get("tileId").Int())] = customData.Get("data").String()
		}

		tilesetByUID[newTS.ID] = newTS

	}

	for index, level := range project.Levels {

		level.Project = project

		if level.BGColorString != "" {
			level.BGColor, _ = parseHexColorFast(level.BGColorString)
		} else {
			level.BGColor = color.RGBA{}
		}

		// Parse level JSON data for background info
		levelData := gjson.Get(dataStr, "levels."+strconv.Itoa(index))

		if levelData.Get("bgRelPath").Exists() && levelData.Get("bgRelPath").String() != "" {

			bgPos := levelData.Get("__bgPos")
			scale := bgPos.Get("scale").Array()
			cropRect := bgPos.Get("cropRect").Array()

			level.BGImage = &BGImage{
				Path:   levelData.Get("bgRelPath").String(),
				ScaleX: scale[0].Float(),
				ScaleY: scale[1].Float(),
				CropRect: []float64{
					cropRect[0].Float(),
					cropRect[1].Float(),
					cropRect[2].Float(),
					cropRect[3].Float(),
				},
			}

		}

		for layerIndex, layer := range level.Layers {

			layer.level = level

			for i, integer := range levelData.Get("layerInstances." + strconv.Itoa(layerIndex) + ".intGridCsv").Array() {

				if integer.Int() != 0 {

					newI := &Integer{
						Value: int(integer.Int()),
						ID:    i,
					}

					y := int(float64(newI.ID) / float64(layer.CellWidth))
					x := newI.ID - y*layer.CellWidth
					newI.Position = []int{x * layer.GridSize, y * layer.GridSize}

					layer.IntGrid = append(layer.IntGrid, newI)

				}

			}

			for _, e := range layer.Entities {
				if e.TileRect != nil {
					e.TileRect.Tileset = tilesetByUID[e.TileRect.TilesetUID]
				}

				e.level = level

				for _, prop := range e.Properties {
					prop.project = project
				}
			}

			layer.Tileset = tilesetByUID[layer.TilesetUID]

		}

	}

	for _, layerDef := range gjson.Get(dataStr, `defs.layers`).Array() {
		if layerDef.Get("type").String() == "IntGrid" {
			for _, value := range layerDef.Get("intGridValues").Array() {
				project.IntGridNames = append(project.IntGridNames, value.Get("identifier").String())
			}
		}
	}

	entityDefinitions := []*EntityDefinition{}
	defsResult := gjson.Get(dataStr, `defs.entities`).Array()
	for _, def := range defsResult {
		b := []byte(def.Raw)
		entityDefinition := &EntityDefinition{}
		if err := json.Unmarshal(b, &entityDefinition); err != nil {
			return nil, err
		}
		if entityDefinition.TileRect != nil {
			entityDefinition.TileRect.Tileset = tilesetByUID[entityDefinition.TileRect.TilesetUID]
		}
		entityDefinitions = append(entityDefinitions, entityDefinition)
	}
	project.EntityDefinitions = entityDefinitions

	return project, err

}

// IntGridConstantByName returns the IntGrid constant index by a named string. If the string is not found,
// -1 is returned.
func (project *Project) IntGridConstantByName(constantName string) int {
	for i, name := range project.IntGridNames {
		if name == constantName {
			return i
		}
	}
	return -1
}

// Just straight up cribbing this Hex > Color Conversion Code from StackOverflow: https://stackoverflow.com/questions/54197913/parse-hex-string-to-image-color
// Otherwise, colors from LDtk are just strings that you can't really do anything with.

var errInvalidFormat = errors.New("invalid format")

func parseHexColorFast(s string) (c color.RGBA, err error) {
	c.A = 0xff

	if s[0] != '#' {
		return c, errInvalidFormat
	}

	hexToByte := func(b byte) byte {
		switch {
		case b >= '0' && b <= '9':
			return b - '0'
		case b >= 'a' && b <= 'f':
			return b - 'a' + 10
		case b >= 'A' && b <= 'F':
			return b - 'A' + 10
		}
		err = errInvalidFormat
		return 0
	}

	switch len(s) {
	case 7:
		c.R = hexToByte(s[1])<<4 + hexToByte(s[2])
		c.G = hexToByte(s[3])<<4 + hexToByte(s[4])
		c.B = hexToByte(s[5])<<4 + hexToByte(s[6])
	case 4:
		c.R = hexToByte(s[1]) * 17
		c.G = hexToByte(s[2]) * 17
		c.B = hexToByte(s[3]) * 17
	default:
		err = errInvalidFormat
	}
	return
}
