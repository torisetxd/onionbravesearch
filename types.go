package onionbravesearch

type Category string

const (
	CategoryWeb  Category = "search"
	CategoryNews Category = "news"
)

type SafeSearch string

const (
	SafeSearchOff      SafeSearch = "off"
	SafeSearchModerate SafeSearch = "moderate"
	SafeSearchStrict   SafeSearch = "strict"
)

type TimeRange string

const (
	TimeAny   TimeRange = ""
	TimeDay   TimeRange = "pd"
	TimeWeek  TimeRange = "pw"
	TimeMonth TimeRange = "pm"
	TimeYear  TimeRange = "py"
)

type Result struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
	Site        string `json:"site,omitempty"`
	DisplayURL  string `json:"display_url,omitempty"`
	Age         string `json:"age,omitempty"`
	Favicon     string `json:"favicon,omitempty"`
	Thumbnail   string `json:"thumbnail,omitempty"`
	Type        string `json:"type,omitempty"`
}

type Response struct {
	Query   string   `json:"query"`
	Results []Result `json:"results"`
	Related []string `json:"related,omitempty"`
}

type SearchOptions struct {
	Category   Category
	Page       int
	Country    string
	UILang     string
	SafeSearch SafeSearch
	TimeRange  TimeRange
	Spellcheck bool
}
