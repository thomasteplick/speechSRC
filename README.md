<h2>Multistage Sampling Rate Conversion for Speech Signals</h2>
<p>
This program is a web application written in Go that makes use of the html/template package to dynamically
create the web page.  Start the web server by issuing bin\speechSRC.exe.  It can be built by navigating to 
src\sampleRateConv\ and issuing go build -o ..\..\bin\speechSRC.exe.  Make sure the go env GOOS is set to "windows".
To use the program, in a web browser, enter "http://127.0.0.1:8080/speechSRCtest".  
</p>

<h3>Configuration for Speech, Frequency Domain, and Sampling Rate Conversion Parameters</h3>
<p>
The checkbox <i>New speech</i> allows you to create a new wav file by speaking into
the machine's microphone.  The checkbox <i>Words Only</i> will silence audio that 
doesn't contain any speech.  For spectrogram plots, this will eliminate the noise, but
for Sampling Rate Conversion, it has no affect.  The checkbox
<i>Sample Rate Converter</i> runs sampling rate conversion processing and creates a sample rate converted
speech wav file that will be displayed in the spectrogram or the time domain.  The resampled speech will also be heard 
through your machines audio system.  If Sample Rate Converter is not selected, the original speech wav file will
be displayed and heard.  To hear the speech on your audio device, the <b>fmedia</b> program should
be installed on your machine and the path inserted in your PATH environment variable.
</p>
<p>
The radio button <i>Time Response</i> displays the time domain of either the speech or 
sampling rate converted speech wav file.  The <i>Spectrogram</i> radio button displays frequency 
versus time of the speech or sampling rate converted speech.  Shades of gray signify the power
present at that frequency; the darker the color, the greater the power at that frequency.
</p>
<p>
For the Frequency Domain Parameters, select the window type for the Discrete Fourier 
Transform and the size of the transform.  The FFT size is fixed at 256 and the window
type should be Rectangle.
</p>
<p>
For the Speech Parameters, the <i>Threshold</i> determines the audio amplitude at which
speech is detected.  The <i>Window</i> is the integration time over which audio power
is summed (energy) to determine if a word is present.  If enough energy is present in the window, 
then a word is present.  This is used for spectrogram <i>Words Only</i>.
</p>
<p>
The Sampling Rate Conversion Parameters has the <i>Upsample</i>, <i>Downsample</i> and <i>Sample Rate Factor</i>.
Upsample and Downsample are integers which specify the conversion ratio.  Alternatively, the
<i>Sample Rate Factor</i> is a fixed point number to two decimal places, which can be used to specify 
the factor to multiply the old sampling rate by.  For instance, setting Upsample to 3 and Downsample to 4
is the same as setting Sample Rate Factor to .75.  The Sample Rate Factor will be converted to Upsample
and Downsample integers.  The <i>Stage Order</i> parameters Upsample and Downsample specify the stage
order, either ascending or descending order.  The Upsample and Downsample inputs are factored to prime
numbers 2, 3, 5, 7, and 11.  These become stages in the sampling conversion chain.  An upsample stage is
followed by a downsample stage with a low pass filter in between them.  Polyphase filters are used to
make the processing efficient; the filtering is always done at the lowest sampling rate; this makes use
of the noble identity which allows the filter and sampler to be interchanged.  If the desired sampling ratio
cannot be converted to a chain of one or more stages employing these primes, then an error is returned in
the status window at the bottom of the webpage.  The best results are obtained with Upsample done in descending
order and Downsample done in ascending order, which are the defaults.  But this also requires the most memory
to store the intermediate results.
</p>
<p>
After the above selections are made, click Submit button and the program will display the selected
waveform and you will hear the audio through your machine's audio device.
</p>
<h3>Speech Signal Processing</h3>
<p>
The desired upsample/downsample ratio is factored into the primes 2, 3, 5, 7, and 11 stages.
The stages are chained together.  Each upsample is followed by the Low Pass FIR filter with 
cutoff frequencies pi/2, pi/3, pi/5, pi/7, or pi/11.  The downsample follows the FIR filter.
Each FIR filter has subfilters, the so-called polyphase filters, which will implement the
sampling rate change and reduce or eliminate frequency aliasing or band duplication that 
accompany downsampling and upsampling.  The particular FIR filter is determined by max(upsample, downsample)
for each stage up/down pair.  For instance, a stage employing up/down = 3/5 would use max(3,5) = 5 and 
the pi/5 FIR filter.  The FIR filters are in the data/ folder and were created with the Parks-McClellan optimal FIR filter
design method or Remez exchange algorithm.  The filters are modest with stopbands at -40dB to -50dB and the orders in the
range 69-99.  The FIR filter transition bandwidth is .05pi
</p>

<h4>Time Domain Speech, "Make America Great Again", original</h4>
![image](https://github.com/user-attachments/assets/c9e580d7-2b3c-41c7-aeb1-208e6e5a2503)
<h4>Spectrogram, "Make America Great Again", original, words only</h4>
![image](https://github.com/user-attachments/assets/8d9e8ce2-a00a-460c-9afa-ca886d5ae112)
<h4>Time Domain Speech, "Make America Great Again, converted</h4>

<h4>Spectrogram, "Make America Great Again", converted, words only</h4>
