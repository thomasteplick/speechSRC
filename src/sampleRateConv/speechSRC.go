/*
Speech Multistage Sampling Rate Converter multistage (SRC).  Resample at an arbitrary I/D ratio,
where I is the interpolator upsample value and D is the decimator downsample value.
Factor I and D into prime factors, pair the factors into rational numbers, and up/down
convert the input audio wav file, each stage output becoming the input of the following
stage.

The converter can also be specified as a decimal factor such as 3.25, which is 13/4.
This in turn will be factored into prime numbers and pairs will form rational numbers
to be used in a stage.
Plot time and frequency domains of the speech.  Hear the audio from WAV files.
*/

package main

import (
	"bufio"
	"fmt"
	"html/template"
	"log"
	"math"
	"math/cmplx"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-audio/audio"
	"github.com/go-audio/wav"
	"github.com/mjibson/go-dsp/fft"
)

const (
	addr              = "127.0.0.1:8080"            // http server listen address
	fileTestingSRC    = "templates/testingSRC.html" // html for testing Sample Rate converter
	patternTestingSRC = "/speechSRCtest"            // http handler for Sample Rate Converter testing
	dataDir           = "data/"                     // directory for the weights and audio wav files
	xlabels           = 11                          // # labels on x axis
	ylabels           = 11                          // # labels on y axis
	rows              = 300                         // rows in canvas
	cols              = 300                         // columns in canvas
	sampleRate        = 8000                        // Hz or samples/sec
	maxSamples        = 10000                       // max audio wav samples = 1.158 sec * sampleRate
	twoPi             = 2.0 * math.Pi               // 2Pi
	bitDepth          = 16                          // audio wav encoder/decoder sample size
	ncolors           = 5                           // number of grayscale colors in spectrogram
	speechTestWav     = "speech.wav"                // Test speech wav file
	speechConvWav     = "speechConv.wav"            // Resampled speech wav file
	nprimes           = 5                           // number of primes used to factor I and D
)

// Type to contain all the HTML template actions
type PlotT struct {
	Grid   []string // plotting grid
	Status string   // status of the plot
	Xlabel []string // x-axis labels
	Ylabel []string // y-axis labels
	Domain string   // plot time or spectrogra domain
}

// Type to hold the minimum and maximum data values
type Endpoints struct {
	xmin float64
	xmax float64
	ymin float64
	ymax float64
}

type Bound struct {
	start, stop int // word boundaries in the message
}

// Primary data structure for holding the Sampling Rate Converter state
type Src struct {
	downsample       int          // D downsample overall, to be factored into primes
	upsample         int          // L interpolator overall, to be factored into primes
	y                [2][]float64 // input/output of src stages that alternate
	deci             []int        // decimator values for each stage
	inter            []int        // interpolator values for each stage
	stages           int          // number of I/D stages
	srf              float64      // sample rate factor
	sf               float64      // storage factor
	lpf              [][]float64  // low pass filters
	file             string
	wordsOnly        bool           // check for presence of words in the audio
	plot             *PlotT         // data to be distributed in the HTML template
	Endpoints                       // embedded struct
	nsamples         int            // number of audio wav samples
	wordWindow       int            // message word window to accumulate audio level
	dbLevel          int            // message word audio level to determine start
	domain           string         // time or spectrogram plot
	grayscale        map[int]string // grayscale for spectrogram
	fftSize          int            // FFT size for spectrogram
	fftWindow        string         // FFT window
	bounds           []Bound        // word boundaries in the audio
	speech           []float64      // input
	convSpeech       []float64      // sample rate converted speech
	convSampleRate   bool           // run sample rate conversion
	sampleRateFactor float64        //  decimal 2-places for I/D
}

// Window function type
type Window func(n int, m int) complex128

// global variables for parse and execution of the html template
var (
	tmplTestingSRC *template.Template
	winType        = []string{"Bartlett", "Welch", "Hamming", "Hanning", "Rectangle"}
	primes         = [nprimes]int{2, 3, 5, 7, 11}
	DI2lpf         = map[int]int{2: 0, 3: 1, 5: 2, 7: 3, 11: 4}
)

// Bartlett window
func bartlett(n int, m int) complex128 {
	real := 1.0 - math.Abs((float64(n)-float64(m))/float64(m))
	return complex(real, 0)
}

// Welch window
func welch(n int, m int) complex128 {
	x := math.Abs((float64(n) - float64(m)) / float64(m))
	real := 1.0 - x*x
	return complex(real, 0)
}

// Hamming window
func hamming(n int, m int) complex128 {
	return complex(.54-.46*math.Cos(math.Pi*float64(n)/float64(m)), 0)
}

// Hanning window
func hanning(n int, m int) complex128 {
	return complex(.5-.5*math.Cos(math.Pi*float64(n)/float64(m)), 0)
}

// Rectangle window
func rectangle(n int, m int) complex128 {
	return 1.0
}

// init parses the html template files
func init() {
	tmplTestingSRC = template.Must(template.ParseFiles(fileTestingSRC))
}

// newSrc constructs a Sampling Rate Converter instance
func newSrc(r *http.Request, plot *PlotT) (*Src, error) {
	// Read the Src parameters in the HTML Form
	txt := r.FormValue("wordwindow")
	if len(txt) == 0 {
		return nil, fmt.Errorf("select Threshold and Window from the Speech Parameter lists")
	}
	window, err := strconv.Atoi(txt)
	if err != nil {
		fmt.Printf("Conversion to int of 'window' error: %v\n", err)
		return nil, err
	}

	txt = r.FormValue("threshold")
	if len(txt) == 0 {
		return nil, fmt.Errorf("select Threshold and Window from the Speech Parameter lists")
	}
	dbLevel, err := strconv.Atoi(txt)
	if err != nil {
		fmt.Printf("Conversion to int of 'threshold' error: %v\n", err)
		return nil, err
	}

	fftWindow := r.FormValue("fftwindow")

	txt = r.FormValue("fftsize")
	fftSize, err := strconv.Atoi(txt)
	if err != nil {
		fmt.Printf("fftsize int conversion error: %v\n", err)
		return nil, err
	}

	wordsOnly := false
	if len(r.FormValue("wordsonly")) > 0 {
		wordsOnly = true
	}

	convSampleRate := false
	if len(r.FormValue("src")) > 0 {
		convSampleRate = true
	}

	// new Src object
	src := Src{
		wordWindow:     window,
		dbLevel:        dbLevel,
		fftSize:        fftSize,
		fftWindow:      fftWindow,
		plot:           plot,
		wordsOnly:      wordsOnly,
		convSampleRate: convSampleRate,
		upsample:       1,
		downsample:     1,
	}
	src.bounds = make([]Bound, 0)

	// Get the low pass filters
	src.lpf = make([][]float64, nprimes)
	files, err := os.ReadDir(dataDir)
	if err != nil {
		fmt.Printf("ReadDir for %s error: %v\n", dataDir, err)
		return nil, fmt.Errorf("ReadDir for %s error %v", dataDir, err.Error())
	}
	n := 0
	for _, dirEntry := range files {
		name := dirEntry.Name()
		base := filepath.Base(name)
		if strings.Contains(base, "lpf") {
			f, err := os.Open(path.Join(dataDir, name))
			if err != nil {
				fmt.Printf("Open %s error: %v\n", name, err)
				return nil, fmt.Errorf("file Open %s error: %v", name, err.Error())
			}
			defer f.Close()
			// lpf_3_51.txt, pi/3, 51 = order, length = order+1
			items := strings.Split(base, "_")
			fcutoff, _ := strconv.Atoi(items[1])
			forder, _ := strconv.Atoi(items[2])
			src.lpf[n] = make([]float64, forder+1)
			DI2lpf[fcutoff] = n
			scanner := bufio.NewScanner(f)
			k := 0
			var coeff float64
			for scanner.Scan() {
				value := scanner.Text()
				if coeff, err = strconv.ParseFloat(value, 64); err != nil {
					fmt.Printf("String %s conversion to float error: %v\n", value, err)
					continue
				}
				src.lpf[n][k] = coeff
				k++
			}
			n++
		}
	}

	// Determine if Sampling Rate Converter is wanted
	smpRateConv := r.FormValue("src")
	if len(smpRateConv) > 0 {
		txt := r.FormValue("upsample")
		if len(txt) > 0 {
			upSample, err := strconv.Atoi(txt)
			if err != nil {
				fmt.Printf("upsample int conversion error: %v\n", err)
				return nil, fmt.Errorf("upsample int conversion error: %s", err.Error())
			}
			txt = r.FormValue("downsample")
			if len(txt) == 0 {
				fmt.Printf("Enter Downsample for Sampling Rate Converter")
				return nil, fmt.Errorf("enter downsample for Sampling Rate Converter")
			}
			downSample, err := strconv.Atoi(txt)
			if err != nil {
				fmt.Printf("downsample int conversion error: %v\n", err)
				return nil, fmt.Errorf("downsample int conversion error: %v", err.Error())
			}
			src.upsample = upSample
			src.downsample = downSample
			src.sampleRateFactor = 0
		} else {
			txt = r.FormValue("sampleratefactor")
			if len(txt) == 0 {
				fmt.Println("Enter Upsample and Downsample or Sample Rate Factor for Sampling Rate Converter")
				return nil, fmt.Errorf("enter Upsample and Downsample or Sample Rate Factor for Sample Rate Converter")
			}
			sampleRateFactor, err := strconv.ParseFloat(txt, 64)
			if err != nil {
				fmt.Printf("Sample Rate Factor float conversion error: %v\n", err)
				return nil, fmt.Errorf("sample rate factor float conversion error: %v", err.Error())
			}
			src.sampleRateFactor = sampleRateFactor
			src.upsample = 0
			src.downsample = 0
		}
		src.file = filepath.Join(dataDir, speechTestWav)
	}
	return &src, nil
}

// Welch's Method and Bartlett's Method variation of the Periodogram
func (src *Src) calculatePSD(audio []float64, PSD []float64, fftWindow string, fftSize int) (float64, float64, error) {

	N := fftSize
	m := N / 2

	// map of window functions
	window := make(map[string]Window, len(winType))
	// Put the window functions in the map
	window["Bartlett"] = bartlett
	window["Welch"] = welch
	window["Hamming"] = hamming
	window["Hanning"] = hanning
	window["Rectangle"] = rectangle

	w, ok := window[fftWindow]
	if !ok {
		fmt.Printf("Invalid FFT window type: %v\n", fftWindow)
		return 0, 0, fmt.Errorf("invalid FFT window type: %v", fftWindow)
	}

	bufN := make([]complex128, N)

	for j := 0; j < len(audio); j++ {
		bufN[j] = complex(audio[j], 0)
	}

	// zero-pad the remaining samples
	for i := len(audio); i < N; i++ {
		bufN[i] = 0
	}

	// window the N samples with chosen window
	for k := 0; k < N; k++ {
		bufN[k] *= w(k, m)
	}

	// Perform N-point complex FFT and add squares to previous values in PSD
	fourierN := fft.FFT(bufN)
	x := cmplx.Abs(fourierN[0])
	PSD[0] = x * x
	psdMax := PSD[0]
	psdAvg := PSD[0]
	for j := 1; j < m; j++ {
		// Use positive and negative frequencies -> bufN[N-j] = bufN[-j]
		xj := cmplx.Abs(fourierN[j])
		xNj := cmplx.Abs(fourierN[N-j])
		PSD[j] = xj*xj + xNj*xNj
		if PSD[j] > psdMax {
			psdMax = PSD[j]
		}
		psdAvg += PSD[j]
	}

	return psdAvg / float64(m), psdMax, nil
}

// findWords finds the word boundaries in the speech
func (src *Src) findWords(filename string) error {

	// prevent oscillation about threshold
	const hystersis = 0.8
	var data []float64 = src.speech
	if filename == speechConvWav {
		data = src.convSpeech
	}

	var (
		old   float64 = 0.0
		new   float64 = 0.0
		cur   int     = 0
		start int     = 0
		stop  int     = 0
		sum   float64 = 0.0
		k     int     = 0
		j     int     = 0
		L     int     = src.nsamples
		max   float64 = 0.0
		avg   float64 = 0.0
	)

	// Find the maximum and normalize the data
	for i := 0; i < L; i++ {
		new = math.Abs(data[i])
		avg += new
		if new > max {
			max = new
		}
	}
	avg /= float64(L)

	// The number of samples in the audio level integration window.
	// Determines when the word and message ends
	// Convert wordWindow to ms
	win := int(float64(src.wordWindow) * .001 / (1.0 / float64(sampleRate*src.upsample/src.downsample)))
	// Minimum audio integration to determine when word begins and ends
	levelSum := float64(win) * avg
	buf := make([]float64, win)

	for k < L {
		for k < L {
			new = math.Abs(data[k])
			old = buf[cur]
			buf[cur] = new
			sum += (new - old)
			cur = (cur + 1) % win
			if k >= stop+win && sum > levelSum {
				start = k - win
				src.bounds = append(src.bounds, Bound{start: start})
				k++
				break
			}
			k++
		}

		for k < L {
			new = math.Abs(data[k])
			old = buf[cur]
			buf[cur] = new
			sum += (new - old)
			cur = (cur + 1) % win
			if k > start+win && sum < levelSum*hystersis {
				stop = k
				src.bounds[j].stop = stop
				k++
				break
			}
			k++
		}
		j++
	}
	//fmt.Printf("in findWords, speech length = %d, bounds = %v, avg = %.2f, max = %.2f\n", len(data), vcdr.bounds, avg, max)
	return nil
}

// findFactors factors I and D and create each stage's up/down converter ratios
func (src *Src) findFactors() error {
	// factor upsample and downsample into primes and I/D for each stage
	// find storage factor which is the maximum amount of storage needed for each stage output
	I := src.upsample
	D := src.downsample
	src.inter = make([]int, 0)
	src.deci = make([]int, 0)

	// make I and D relatively prime by removing common factors

	prevI := 0
	for prevI != I {
		prevI = I
		for _, pr := range primes {
			if (I%pr == 0) && (D%pr == 0) {
				I /= pr
				D /= pr
				break
			}
		}
	}

	// loop over primes and factor I and D until the quotient is one
	if I == 1 {
		src.inter = append(src.inter, 1)
	} else {
		quo := I
		for quo != 1 {
			found := 0
			for _, pr := range primes {
				if quo%pr == 0 {
					src.inter = append(src.inter, pr)
					quo /= pr
					found++
					break
				}
			}
			if found == 0 {
				break
			}
		}
	}

	if D == 1 {
		src.deci = append(src.deci, 1)
	} else {
		quo := D
		for quo != 1 {
			found := 0
			for _, pr := range primes {
				if quo%pr == 0 {
					src.deci = append(src.deci, pr)
					quo /= pr
					found++
					break
				}
			}
			if found == 0 {
				break
			}
		}
	}

	if len(src.deci) == 0 || len(src.inter) == 0 {
		return fmt.Errorf("decimator or interpolator not factorable into prime numbers")
	}

	// find the number of stages
	src.stages = max(len(src.inter), len(src.deci))

	// pad deci or inter with ones to make them the same length
	for len(src.deci) < src.stages {
		src.deci = append(src.deci, 1)
	}
	for len(src.inter) < src.stages {
		src.inter = append(src.inter, 1)
	}

	// find storage factor sf that is the amount of memory needed for
	// storing the stages input and output
	src.sf = float64(src.nsamples)
	max := float64(src.nsamples)
	for i, k := range src.inter {
		src.sf *= float64(k) / float64(src.deci[i])
		if src.sf > max {
			max = src.sf
		}
	}
	src.sf = max

	return nil
}

// srf2ID converts the sampling rate factor into the I and D of the multistage SRC
func (src *Src) srf2ID() error {
	k := src.srf
	const N = 100
	const eps = 0.1
	for i := 1; i <= N; i++ {
		tmp := float64(i) * k
		if tmp-math.Floor(tmp) < eps {
			src.upsample = int(tmp)
			src.downsample = i
			return nil
		}
	}
	return fmt.Errorf("no sample ratio I/D for %f", k)
}

// convertSampleRate performs sampling rate conversion in stages consisting of rational numbers of primes
func (src *Src) convertSampleRate() error {

	// find L and M if sampleRateFactor is used
	if src.sampleRateFactor != 0 {
		err := src.srf2ID()
		if err != nil {
			fmt.Printf("sampleRateFactor error: %v\n", err.Error())
			return fmt.Errorf("sampleRateFactor error: %v", err.Error())
		}
	}

	// factor L/M into primes and L/M for each stage
	// find storage factor which is the maximum amount of storage needed for each stage output
	err := src.findFactors()
	if err != nil {
		fmt.Printf("findFactors error: %v\n", err.Error())
		return fmt.Errorf("findFactors error: %v", err.Error())
	}

	// allocate src.y[i] = src.storageFactor*src.nsamples
	src.y[0] = make([]float64, int(math.Ceil(float64(src.nsamples)*src.sf)))
	src.y[1] = make([]float64, int(math.Ceil(float64(src.nsamples)*src.sf)))

	// copy src.speech to src.y[0]
	// loop over SRC stages
	// find FIR filter h with lowest cutoff frequency, min(pi/D, pi/I) or max(D, I)
	// swap input and output buffers after each stage is complete
	// copy resampled from last stage  output buffer to src.convSpeech
	nsamples := src.nsamples
	prev := 0
	cur := 1
	copy(src.y[prev], src.speech)
	for stg := range src.stages {
		// decimator (D) and interpolator (I) factors for this stage
		D := src.deci[stg]
		I := src.inter[stg]
		di := max(D, I)
		h := src.lpf[DI2lpf[di]]
		// loop over samples
		smp := 0
		// length of each polyphase filter in this stage
		K := len(h) / I
		// find polyphase filter in the FIR filter
		// polyphase filter: 0, 1, ..., I-1
		pf := 0
		i := 0
		for smp < nsamples {
			// loop over the polyphase filters
			for i = pf; i < I; i += D {
				sum := 0.0
				// perform convolution using polyphase filter and input y[prev]
				for j := 0; j < K; j++ {
					sum += h[i+j*D] * src.y[prev][smp-j]
				}
				src.y[cur][smp] = sum
			}
			pf = i % I
			smp += (i / I)
		} // this stage is done
		// swap the input and output buffers
		cur, prev = prev, cur
		nsamples = int(float64(nsamples) * float64(I) / float64(D))
	}
	// copy the last stage output to convSpeech for uses elsewhere
	copy(src.convSpeech, src.y[src.stages%2])

	// Create new wav file: save convSpeech to disk
	outF, err := os.Create(path.Join(dataDir, speechConvWav))
	if err != nil {
		fmt.Printf("os.Create() file %s error: %v\n", speechConvWav, err)
		return fmt.Errorf("os.Create() file %s error: %v", speechConvWav, err)
	}
	defer outF.Close()
	// create wav.Encoder
	enc := wav.NewEncoder(outF, sampleRate*src.upsample/src.downsample, bitDepth, 1, 1)

	// create audio.FloatBuffer
	float64Buf := &audio.FloatBuffer{Data: src.convSpeech, Format: &audio.Format{NumChannels: 1, SampleRate: sampleRate}}

	// create IntBuffer from FloatBuffer and pass to Encoder.Write()
	if err := enc.Write(float64Buf.AsIntBuffer()); err != nil {
		fmt.Printf("wav encoder write error: %v\n", err)
		return fmt.Errorf("wav encoder write error: %v", err.Error())
	}

	// close the encoder
	if err := enc.Close(); err != nil {
		fmt.Printf("wav encoder close error: %v\n", err)
		return fmt.Errorf("wav encoder error: %v", err.Error())
	}

	return nil
}

// handleTestingSrc constructs a Src and plots time domain or spectrogram
func handleTestingSrc(w http.ResponseWriter, r *http.Request) {
	// open and read the audio wav file
	// create wav decoder, audio IntBuffer, convert to audio FloatBuffer
	// loop over the Float Buffer data and generate the spectrogram
	// fill the grid with the values
	// Option to plot time domain added.
	// Option to plot spectrogram output added.

	var (
		plot PlotT
		src  *Src
		err  error
	)

	// Construct Sampling Rate Converter instance containing state variables
	src, err = newSrc(r, &plot)
	if err != nil {
		fmt.Printf("newSrc() error: %v\n", err)
		plot.Status = fmt.Sprintf("newSrc() error: %v", err.Error())
		// Write to HTTP using template and grid
		if err := tmplTestingSRC.Execute(w, plot); err != nil {
			log.Fatalf("Write to HTTP output using template with error: %v\n", err)
		}
		return
	}

	// Create the audio speech wav file, otherwise use what is already present
	newMsg := r.FormValue("speech")
	if newMsg == "new" {
		fmedia, err := exec.LookPath("fmedia.exe")
		if err != nil {
			log.Fatal("fmedia is not available in PATH")
		} else {
			fmt.Printf("fmedia is available in path: %s\n", fmedia)
			cmd := exec.Command(fmedia, "--record", "-o", filepath.Join(dataDir, speechTestWav), "--until=5",
				"--format=int16", "--channels=mono", "--rate=8000", "-y", "--start-dblevel=-70", "--stop-dblevel=-30;1")
			stdoutStderr, err := cmd.CombinedOutput()
			if err != nil {
				fmt.Printf("stdout, stderr error from running fmedia: %v\n", err.Error())
				plot.Status = fmt.Sprintf("stdout, stderr error from running fmedia: %v", err.Error())
				// Write to HTTP using template and grid
				if err := tmplTestingSRC.Execute(w, plot); err != nil {
					log.Fatalf("Write to HTTP output using template with error: %v", err)
				}
				return
			} else {
				fmt.Printf("fmedia output: %s\n", string(stdoutStderr))
			}
		}
	}

	// open speech WAV file and convert 16-bit samples to []float64
	// Open the testing message
	f, err := os.Open(filepath.Join(dataDir, speechTestWav))
	if err != nil {
		fmt.Printf("Open file %s error: %v", speechTestWav, err)
		plot.Status = fmt.Sprintf("Open file %s error: %v", speechTestWav, err)
		// Write to HTTP using template and grid
		if err := tmplTestingSRC.Execute(w, plot); err != nil {
			log.Fatalf("Write to HTTP output using template with error: %v\n", err)
		}
		return
	}
	defer f.Close()

	// Create wav Decoder, intBuf, fltBuf and Decode the wav file
	dec := wav.NewDecoder(f)
	bufInt := audio.IntBuffer{
		Format: &audio.Format{NumChannels: 1, SampleRate: sampleRate},
		Data:   make([]int, 2*maxSamples), SourceBitDepth: bitDepth}
	nsamples, err := dec.PCMBuffer(&bufInt)
	if err != nil {
		fmt.Printf("PCMBuffer error: %v\n", err)
		plot.Status = fmt.Sprintf("PCMBuffer error: %v\n", err)
		// Write to HTTP using template and grid
		if err := tmplTestingSRC.Execute(w, plot); err != nil {
			log.Fatalf("Write to HTTP output using template with error: %v\n", err)
		}
		return
	}
	src.speech = bufInt.AsFloatBuffer().Data
	src.nsamples = nsamples
	src.convSpeech = make([]float64, int(float64(src.nsamples)*float64(src.upsample)/float64(src.downsample)))

	// Determine if Sampling Rate Converter processing is wanted
	sampleRateConvert := r.FormValue("src")
	if len(sampleRateConvert) > 0 {
		// Perform Sample Rate Converter processing
		err = src.convertSampleRate()
		if err != nil {
			fmt.Printf("lpc.ProcessSpeech error: %v\n", err)
			plot.Status = fmt.Sprintf("lpc.ProcessSpeech error: %s", err.Error())
			// Write to HTTP using template and grid
			if err := tmplTestingSRC.Execute(w, plot); err != nil {
				log.Fatalf("Write to HTTP output using template with error: %v\n", err)
			}
			return
		}
	}

	// Determine if time or spectrogram domain plot
	src.domain = r.FormValue("domain")
	if len(src.domain) == 0 {
		src.domain = "time"
	}

	file := speechTestWav
	if len(src.domain) > 0 {
		file = speechConvWav
	}

	if src.domain == "spectrogram" {
		src.plot.Domain = "Spectrogram (Hz/sec)"
		src.grayscale = make(map[int]string)
		for i := 0; i < ncolors; i++ {
			src.grayscale[i] = fmt.Sprintf("gs%d", i)
		}

		err = src.processSpectrogram(file, src.fftWindow, src.fftSize)
		if err != nil {
			fmt.Printf("proessSpectrogram error: %v\n", err)
			plot.Status = fmt.Sprintf("processSpectrogram error: %v", err.Error())
			// Write to HTTP using template and grid
			if err := tmplTestingSRC.Execute(w, plot); err != nil {
				log.Fatalf("Write to HTTP output using template with error: %v\n", err)
			}
			return
		}
		plot.Status = "Spectrogram plotted."
	} else {
		src.plot.Domain = "Time Domain (sec)"
		err := src.processTimeDomain(file)
		if err != nil {
			fmt.Printf("processTimeDomain error: %v\n", err)
			plot.Status = fmt.Sprintf("processTimeDomain error: %v", err.Error())
			// Write to HTTP using template and grid
			if err := tmplTestingSRC.Execute(w, plot); err != nil {
				log.Fatalf("Write to HTTP output using template with error: %v\n", err)
			}
			return
		}
		plot.Status = fmt.Sprintf("Time Domain of %s plotted.", filepath.Join(dataDir, file))
	}

	// Play the audio wav if fmedia is available in the PATH environment variable
	fmedia, err := exec.LookPath("fmedia.exe")
	if err != nil {
		log.Fatal("fmedia is not available in PATH")
	} else {
		fmt.Printf("fmedia is available in path: %s\n", fmedia)
		cmd := exec.Command(fmedia, filepath.Join(dataDir, file))
		stdoutStderr, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Printf("stdout, stderr error from running fmedia: %v\n", err)
		} else {
			fmt.Printf("fmedia output: %s\n", string(stdoutStderr))
		}
	}

	// Execute data on HTML template
	if err = tmplTestingSRC.Execute(w, src.plot); err != nil {
		log.Fatalf("Write to HTTP output using template with error: %v\n", err)
	}
}

// findEndpoints finds the minimum and maximum data values
func (ep *Endpoints) findEndpoints(input []float64) {
	ep.ymax = -math.MaxFloat64
	ep.ymin = math.MaxFloat64
	for _, y := range input {

		if y > ep.ymax {
			ep.ymax = y
		}
		if y < ep.ymin {
			ep.ymin = y
		}
	}
}

// processTimeDomain plots the time domain data from audio wav file
func (src *Src) processTimeDomain(filename string) error {

	var (
		xscale    float64
		yscale    float64
		endpoints Endpoints
		data      []float64 = src.speech
	)

	src.plot.Grid = make([]string, rows*cols)
	src.plot.Xlabel = make([]string, xlabels)
	src.plot.Ylabel = make([]string, ylabels)

	if filename == speechConvWav {
		data = src.convSpeech
	}

	endpoints.findEndpoints(data)
	// time starts at 0 and ends at #samples*sampling period
	endpoints.xmin = 0.0
	// #samples*sampling period, sampling period = 1/sampleRate
	endpoints.xmax = float64(src.nsamples) / float64(sampleRate*src.upsample/src.downsample)

	// EP means endpoints
	lenEPx := endpoints.xmax - endpoints.xmin
	lenEPy := endpoints.ymax - endpoints.ymin
	prevTime := 0.0
	prevAmpl := data[0]

	// Calculate scale factors for x and y
	xscale = float64(cols-1) / (endpoints.xmax - endpoints.xmin)
	yscale = float64(rows-1) / (endpoints.ymax - endpoints.ymin)

	// This previous cell location (row,col) is on the line (visible)
	row := int((endpoints.ymax-data[0])*yscale + .5)
	col := int((0.0-endpoints.xmin)*xscale + .5)
	src.plot.Grid[row*cols+col] = "online"

	// Store the amplitude in the plot Grid
	for n := 1; n < src.nsamples; n++ {
		// Current time
		currTime := float64(n) / float64(sampleRate*src.upsample/src.downsample)

		// This current cell location (row,col) is on the line (visible)
		row := int((endpoints.ymax-data[n])*yscale + .5)
		col := int((currTime-endpoints.xmin)*xscale + .5)
		src.plot.Grid[row*cols+col] = "online"

		// Interpolate the points between previous point and current point;
		// draw a straight line between points.
		lenEdgeTime := math.Abs((currTime - prevTime))
		lenEdgeAmpl := math.Abs(data[n] - prevAmpl)
		ncellsTime := int(float64(cols) * lenEdgeTime / lenEPx) // number of points to interpolate in x-dim
		ncellsAmpl := int(float64(rows) * lenEdgeAmpl / lenEPy) // number of points to interpolate in y-dim
		// Choose the biggest
		ncells := ncellsTime
		if ncellsAmpl > ncells {
			ncells = ncellsAmpl
		}

		stepTime := float64(currTime-prevTime) / float64(ncells)
		stepAmpl := float64(data[n]-prevAmpl) / float64(ncells)

		// loop to draw the points
		interpTime := prevTime
		interpAmpl := prevAmpl
		for i := 0; i < ncells; i++ {
			row := int((endpoints.ymax-interpAmpl)*yscale + .5)
			col := int((interpTime-endpoints.xmin)*xscale + .5)
			// This cell location (row,col) is on the line (visible)
			src.plot.Grid[row*cols+col] = "online"
			interpTime += stepTime
			interpAmpl += stepAmpl
		}

		// Update the previous point with the current point
		prevTime = currTime
		prevAmpl = data[n]

	}

	// Set plot status if no errors
	if len(src.plot.Status) == 0 {
		src.plot.Status = fmt.Sprintf("file %s plotted from (%.3f,%.3f) to (%.3f,%.3f)",
			filename, endpoints.xmin, endpoints.ymin, endpoints.xmax, endpoints.ymax)
	}

	// Construct x-axis labels
	incr := (endpoints.xmax - endpoints.xmin) / (xlabels - 1)
	x := endpoints.xmin
	// First label is empty for alignment purposes
	for i := range src.plot.Xlabel {
		src.plot.Xlabel[i] = fmt.Sprintf("%.2f", x)
		x += incr
	}

	// Construct the y-axis labels
	incr = (endpoints.ymax - endpoints.ymin) / (ylabels - 1)
	y := endpoints.ymin
	for i := range src.plot.Ylabel {
		src.plot.Ylabel[i] = fmt.Sprintf("%.2f", y)
		y += incr
	}

	return nil
}

// inBoundsSample checks if the sample is inside word boundaries
func (src *Src) inBoundsSample(smpl int, margin int) bool {
	for _, bound := range src.bounds {
		if smpl > (bound.start-margin) && smpl < (bound.stop-margin) {
			return true
		}
	}
	return false
}

// processSpectrogram creates a spectrogram of the speech waveform
func (src *Src) processSpectrogram(filename, fftWindow string, fftSize int) error {

	// get audio samples from audio wav file
	// open and read the audio wav file
	// create wav decoder, audio IntBuffer, convert IntBuffer to audio FloatBuffer
	var (
		endpoints Endpoints
		PSD       []float64 // power spectral density
		xscale    float64   // data to grid in x direction
		yscale    float64   // data to grid in y direction
		data      []float64 = src.speech
	)

	fftSize2 := src.fftSize / 2
	if filename == speechConvWav {
		data = src.convSpeech
	}

	src.plot.Grid = make([]string, rows*cols)
	src.plot.Xlabel = make([]string, xlabels)
	src.plot.Ylabel = make([]string, ylabels)

	// Power Spectral Density, PSD[N/2] is the Nyquist critical frequency
	// It is (sampling frequency)/2, the highest non-aliased frequency
	PSD = make([]float64, fftSize2)

	// x-axis is time or sample, y-axis is frequency
	endpoints.xmin = 0.0
	endpoints.xmax = float64(src.nsamples)
	endpoints.ymin = 0.0
	endpoints.ymax = float64(fftSize2) // equivalent to Nyquist critical frequency

	// Calculate scale factors to convert physical units to screen units
	xscale = float64(cols-1) / (endpoints.xmax - endpoints.xmin)
	yscale = float64(rows-1) / (endpoints.ymax - endpoints.ymin)

	// number of cells to interpolate in time and frequency
	// round up so the cells in the plot grid are connected
	ncellst := int((math.Ceil(float64(cols) * float64(fftSize2) / float64(src.nsamples))))
	ncellsf := int(math.Ceil(float64(rows) / float64(fftSize2)))

	stepTime := float64((fftSize2) / ncellst)
	stepFreq := 1.0 / float64(ncellsf)

	// if wordsOnly, only do calculatePSD for samples inside the word boundaries to minimize
	// checking the spectrum of noise.  This would give a broad range of frequencies which
	// is not of interest.
	if src.wordsOnly {
		// loop over fltBuf and find the speech bounds
		err := src.findWords(filename)
		if err != nil {
			fmt.Printf("findWords error: %v", err)
			return fmt.Errorf("findWords error: %s", err.Error())
		}
	}
	fmt.Printf("word boundaries:%v\n", src.bounds)

	// for loop over samples, increment by fftSize/2, calculatePSD on the batch
	// Overlap by 50% due to non-rectangular window to avoid Gibbs phenomenon
	for smpl := 0; smpl < src.nsamples; smpl += fftSize2 {
		if !src.wordsOnly || src.inBoundsSample(smpl, fftSize2) {
			// calculate the PSD using Bartlett's or Welch's variant of the Periodogram
			end := smpl + fftSize
			if end > src.nsamples {
				end = src.nsamples
			}
			_, psdMax, err := src.calculatePSD(data[smpl:end], PSD, fftWindow, fftSize)
			if err != nil {
				fmt.Printf("calculatePSD error: %v\n", err)
				return fmt.Errorf("calculatePSD error: %v", err.Error())
			}

			// for loop over the frequency bins in the PSD
			for bin := 0; bin < fftSize2; bin++ {
				// find the grayscale color based on bin power
				// largest power is black, smallest power is white
				// shades of gray in-between black and white
				var gs string
				r := PSD[bin] / psdMax
				if r < .1 {
					gs = src.grayscale[4]
				} else if r < .25 {
					gs = src.grayscale[3]
				} else if r < .5 {
					gs = src.grayscale[2]
				} else if r < .8 {
					gs = src.grayscale[1]
				} else {
					gs = src.grayscale[0]
				}

				// interpolate in time
				interpTime := float64(smpl)
				for nct := 0; nct < ncellst; nct++ {
					col := int((interpTime-endpoints.xmin)*xscale + .5)
					if col >= cols {
						col = cols - 1
					}
					// interpolate in frequency
					interpFreq := float64(bin)
					for ncf := 0; ncf < ncellsf; ncf++ {
						row := int((endpoints.ymax-interpFreq)*yscale + .5)
						if row < 0 {
							row = 0
						}
						// Store the color in the plot Grid
						src.plot.Grid[row*cols+col] = gs
						interpFreq += stepFreq
					}
					interpTime += stepTime
				}
			}
		}
	}

	// Construct x-axis labels
	incr := (endpoints.xmax - endpoints.xmin) / float64((xlabels - 1) * (sampleRate*src.upsample/src.downsample))
	x := endpoints.xmin / float64(sampleRate*src.upsample/src.downsample)
	// First label is empty for alignment purposes
	for i := range src.plot.Xlabel {
		src.plot.Xlabel[i] = fmt.Sprintf("%.2f", x)
		x += incr
	}

	// Apply the  sampling rate in Hz to the y-axis using a scale factor
	// Convert the fft size to sampleRate/2, the Nyquist critical frequency
	sf := 0.5 * float64(sampleRate*src.upsample/src.downsample) / endpoints.ymax

	// Construct y-axis labels
	incr = (endpoints.ymax - endpoints.ymin) / (ylabels - 1)
	y := endpoints.ymin
	// First label is empty for alignment purposes
	for i := range src.plot.Ylabel {
		src.plot.Ylabel[i] = fmt.Sprintf("%.0f", y*sf)
		y += incr
	}

	return nil
}

// executive creates the HTTP handlers, listens on addr, and serves the HTML
func main() {
	// Set up HTTP servers with handlers for testing the multistage sampling rate converter

	// Create HTTP handler for SRC testing
	http.HandleFunc(patternTestingSRC, handleTestingSrc)
	fmt.Printf("Multistage Sampling Rate Converter Server listening on %v.\n", addr)
	http.ListenAndServe(addr, nil)
}
