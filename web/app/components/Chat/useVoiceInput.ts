'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import type { VoiceInput, WaveformMode } from './Chat.types';

const PAUSE_TIMEOUT_MS = 1500;

export interface UseVoiceInputOptions {
  /** Called with the final transcript when speech pauses or recording stops */
  onTranscript: (text: string) => void;
}

function getSpeechRecognitionCtor(): (new () => SpeechRecognition) | null {
  if (typeof window === 'undefined') {
    return null;
  }
  return (window as any).SpeechRecognition ?? (window as any).webkitSpeechRecognition ?? null;
}

export function useVoiceInput({ onTranscript }: UseVoiceInputOptions): VoiceInput {
  const [isRecording, setIsRecording] = useState(false);
  const [transcript, setTranscript] = useState('');
  const [analyser, setAnalyser] = useState<AnalyserNode | null>(null);
  const [waveformMode, setWaveformMode] = useState<WaveformMode>('wave');

  // Keep stable references
  const onTranscriptRef = useRef(onTranscript);
  onTranscriptRef.current = onTranscript;

  const recognitionRef = useRef<SpeechRecognition | null>(null);
  const audioCtxRef = useRef<AudioContext | null>(null);
  const streamRef = useRef<MediaStream | null>(null);
  const pauseTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const finalTranscriptRef = useRef('');
  const isStoppingRef = useRef(false);

  const [isSupported, setIsSupported] = useState(false);

  useEffect(() => {
    setIsSupported(getSpeechRecognitionCtor() !== null);
  }, []);

  const cleanup = useCallback(() => {
    if (pauseTimerRef.current) {
      clearTimeout(pauseTimerRef.current);
      pauseTimerRef.current = null;
    }

    if (recognitionRef.current) {
      recognitionRef.current.onresult = null;
      recognitionRef.current.onend = null;
      recognitionRef.current.onerror = null;
      try {
        recognitionRef.current.abort();
      } catch {
        // already stopped
      }
      recognitionRef.current = null;
    }

    if (streamRef.current) {
      streamRef.current.getTracks().forEach((t) => t.stop());
      streamRef.current = null;
    }

    if (audioCtxRef.current) {
      audioCtxRef.current.close().catch(() => {});
      audioCtxRef.current = null;
    }

    setAnalyser(null);
    setIsRecording(false);
    setTranscript('');
    finalTranscriptRef.current = '';
    isStoppingRef.current = false;
  }, []);

  const submitAndCleanup = useCallback(() => {
    const text = finalTranscriptRef.current.trim();
    cleanup();
    if (text) {
      onTranscriptRef.current(text);
    }
  }, [cleanup]);

  const start = useCallback(async () => {
    const Ctor = getSpeechRecognitionCtor();
    if (!Ctor) {
      return;
    }

    // Cleanup any previous session
    cleanup();

    // Set up audio context + analyser for waveform
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      streamRef.current = stream;

      const audioCtx = new AudioContext();
      audioCtxRef.current = audioCtx;

      const source = audioCtx.createMediaStreamSource(stream);
      const analyserNode = audioCtx.createAnalyser();
      analyserNode.fftSize = 256;
      source.connect(analyserNode);
      // Do NOT connect to audioCtx.destination (would cause feedback)

      setAnalyser(analyserNode);
    } catch {
      // Microphone denied — can't visualize but can still recognize
    }

    // Set up SpeechRecognition
    const recognition = new Ctor();
    recognition.continuous = true;
    recognition.interimResults = true;
    recognition.lang = 'en-US';
    recognitionRef.current = recognition;

    recognition.onresult = (event: SpeechRecognitionEvent) => {
      let final = '';
      let interim = '';

      for (let i = 0; i < event.results.length; i++) {
        const result = event.results[i];
        if (result.isFinal) {
          final += result[0].transcript;
        } else {
          interim += result[0].transcript;
        }
      }

      finalTranscriptRef.current = final;
      setTranscript(final + interim);

      // Reset pause timer on every result
      if (pauseTimerRef.current) {
        clearTimeout(pauseTimerRef.current);
      }

      // Only auto-submit on pause if we have final text
      if (final.trim()) {
        pauseTimerRef.current = setTimeout(() => {
          isStoppingRef.current = true;
          recognitionRef.current?.stop();
        }, PAUSE_TIMEOUT_MS);
      }
    };

    recognition.onend = () => {
      // Called when recognition stops (either by us or by the browser)
      submitAndCleanup();
    };

    recognition.onerror = (event: SpeechRecognitionErrorEvent) => {
      // 'no-speech' and 'aborted' are non-fatal in continuous mode
      if (event.error === 'no-speech' || event.error === 'aborted') {
        return;
      }
      cleanup();
    };

    recognition.start();
    setIsRecording(true);
  }, [cleanup, submitAndCleanup]);

  const stop = useCallback(() => {
    if (!recognitionRef.current) {
      return;
    }
    isStoppingRef.current = true;
    recognitionRef.current.stop();
    // onend handler will call submitAndCleanup
  }, []);

  const toggleWaveformMode = useCallback(() => {
    setWaveformMode((m) => (m === 'bars' ? 'wave' : 'bars'));
  }, []);

  // Cleanup on unmount
  useEffect(() => {
    return cleanup;
  }, [cleanup]);

  return {
    isSupported,
    isRecording,
    transcript,
    analyser,
    waveformMode,
    start,
    stop,
    toggleWaveformMode,
  };
}
