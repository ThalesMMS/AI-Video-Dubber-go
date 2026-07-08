package transcription

import "testing"

func TestCuesFromWhisperResultFiltersLikelyNoSpeechHallucinations(t *testing.T) {
	result := whisperResult{Segments: []whisperSegment{
		{
			Start:        0,
			End:          6.6,
			Text:         " Oh yeah, we shall go one and yeah, one and yeah",
			NoSpeechProb: 0.59576416015625,
			AvgLogProb:   -0.9968271255493164,
			Compression:  1.8043478260869565,
		},
	}}

	cues := cuesFromWhisperResult(result)
	if len(cues) != 0 {
		t.Fatalf("cues = %#v, want no cues for likely no-speech hallucination", cues)
	}
}

func TestCuesFromWhisperResultKeepsConfidentSpeech(t *testing.T) {
	result := whisperResult{Segments: []whisperSegment{
		{
			Start:        1.25,
			End:          3.5,
			Text:         " Ultrasound uses sound waves.",
			NoSpeechProb: 0.08,
			AvgLogProb:   -0.21,
			Compression:  1.2,
		},
	}}

	cues := cuesFromWhisperResult(result)
	if len(cues) != 1 {
		t.Fatalf("len(cues) = %d, want 1", len(cues))
	}
	if cues[0].Text != "Ultrasound uses sound waves." {
		t.Fatalf("cue text = %q", cues[0].Text)
	}
}
