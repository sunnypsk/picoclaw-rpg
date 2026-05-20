package turtlesoup

type Puzzle struct {
	ID         string   `json:"id"`
	Surface    string   `json:"surface"`
	Solution   string   `json:"solution"`
	Hints      []string `json:"hints,omitempty"`
	Difficulty string   `json:"difficulty,omitempty"`
	Themes     []string `json:"themes,omitempty"`
}
