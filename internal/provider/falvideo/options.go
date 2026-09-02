package falvideo

// Options configures Gemini Omni Flash video generation through fal.
type Options struct {
	Prompt           string
	Model            string
	Images           []string
	Videos           []string
	Audios           []string
	FirstFrame       string
	LastFrame        string
	Duration         int
	Resolution       string
	AspectRatio      string
	GenerateAudio    bool
	Out              string
	NoWait           bool
	PollIntervalSecs int
	MaxWaitSecs      int
}
