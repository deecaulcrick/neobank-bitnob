import { Canvas, Fill, Shader, Skia, useClock } from '@shopify/react-native-skia';
import { useState } from 'react';
import { StyleSheet, View } from 'react-native';
import { useDerivedValue, useReducedMotion } from 'react-native-reanimated';

import type { SkyMode } from '../lib/prefs';
import { colors } from '../theme';

const NOISE = `
uniform float2 resolution;
uniform float time;

float hash(float2 p) {
  p = fract(p * float2(123.34, 456.21));
  p += dot(p, p + 45.32);
  return fract(p.x * p.y);
}

float noise(float2 p) {
  float2 i = floor(p);
  float2 f = fract(p);
  f = f * f * (3.0 - 2.0 * f);
  float a = hash(i);
  float b = hash(i + float2(1.0, 0.0));
  float c = hash(i + float2(0.0, 1.0));
  float d = hash(i + float2(1.0, 1.0));
  return mix(mix(a, b, f.x), mix(c, d, f.x), f.y);
}

float fbm(float2 p) {
  float v = 0.0;
  float a = 0.5;
  for (int i = 0; i < 5; i++) {
    v += a * noise(p);
    p = p * 2.02 + float2(17.0, 9.0);
    a *= 0.5;
  }
  return v;
}
`;

// Day: clear blue deepening overhead, a soft sun, slow white cloud.
const day = Skia.RuntimeEffect.Make(`${NOISE}
half4 main(float2 xy) {
  float2 uv = xy / resolution;
  float2 p = float2(uv.x * resolution.x / resolution.y, uv.y);
  float t = time * 0.02;

  // Zenith to horizon.
  float3 col = mix(float3(0.07, 0.33, 0.86), float3(0.32, 0.62, 0.97), smoothstep(0.0, 0.75, uv.y));
  col = mix(col, float3(0.66, 0.85, 1.0), smoothstep(0.6, 1.0, uv.y) * 0.7);

  // Sun, off to the upper right.
  float sun = distance(p, float2(0.82 * resolution.x / resolution.y, 0.1));
  col += float3(1.0, 0.95, 0.8) * exp(-sun * sun * 7.0) * 0.3;

  // Cloud, thicker toward the horizon, with a little shade underneath.
  float c1 = fbm(float2(p.x * 1.6, p.y * 3.2) + float2(t, 0.0));
  float c2 = fbm(float2(p.x * 3.4, p.y * 6.0) + float2(t * 1.7, t * 0.2) + c1);
  float cloud = smoothstep(0.52, 0.82, c1 * 0.62 + c2 * 0.5 + uv.y * 0.1);
  float shade = smoothstep(0.5, 0.9, fbm(float2(p.x * 3.4, p.y * 6.0 - 0.12) + float2(t * 1.7, t * 0.2) + c1));
  col = mix(col, mix(float3(1.0), float3(0.8, 0.88, 0.98), shade * 0.5), cloud * 0.8);

  col += (hash(xy) - 0.5) * 0.015;
  return half4(col, 1.0);
}
`)!;

// Night: deep gradient, slow drifting cloud glow, a lime haze on the horizon
// where the sheet begins, and a few twinkling stars.
const night = Skia.RuntimeEffect.Make(`${NOISE}
half4 main(float2 xy) {
  float2 uv = xy / resolution;
  float2 p = float2(uv.x * resolution.x / resolution.y, uv.y);
  float t = time * 0.035;

  // Zenith to horizon.
  float3 col = mix(float3(0.027, 0.035, 0.043), float3(0.045, 0.085, 0.125), smoothstep(0.0, 0.6, uv.y));
  col = mix(col, float3(0.13, 0.25, 0.27), smoothstep(0.4, 1.0, uv.y) * 0.9);

  // Two layers of cloud, the second warped by the first.
  float c1 = fbm(p * 2.2 + float2(t, -t * 0.4));
  float c2 = fbm(p * 4.0 - float2(t * 1.3, 0.0) + c1);
  float cloud = smoothstep(0.45, 0.9, c1 * 0.65 + c2 * 0.5);
  col += cloud * float3(0.11, 0.18, 0.24) * (0.4 + uv.y);

  // Horizon haze in the brand lime, breathing slowly along its length.
  float haze = exp(-pow((uv.y - 1.0) * 2.4, 2.0));
  haze *= 0.45 + 0.55 * fbm(float2(p.x * 1.6 + t * 2.0, 3.0));
  col += haze * float3(0.36, 0.5, 0.16) * 0.6;

  // Stars: at most one per grid cell, dimmer near the horizon and in cloud.
  float2 cell = floor(xy / 14.0);
  float seed = hash(cell);
  if (seed > 0.93) {
    float2 centre = (cell + 0.2 + 0.6 * float2(hash(cell + 3.1), hash(cell + 7.7))) * 14.0;
    float twinkle = 0.6 + 0.4 * sin(time * (0.6 + seed) + seed * 40.0);
    float star = smoothstep(1.3, 0.0, distance(xy, centre)) * twinkle;
    col += star * (1.0 - smoothstep(0.35, 0.95, uv.y)) * (1.0 - cloud) * 0.9;
  }

  // Fixed grain so the gradient doesn't band.
  col += (hash(xy) - 0.5) * 0.02;
  return half4(col, 1.0);
}
`)!;

const BASE: Record<SkyMode, string> = { day: '#2F74E0', night: colors.night };

export function Sky({ mode }: { mode: SkyMode }) {
  const [size, setSize] = useState({ width: 0, height: 0 });
  const reduceMotion = useReducedMotion();
  const clock = useClock();

  const uniforms = useDerivedValue(
    () => ({
      resolution: [size.width, size.height],
      // Seconds; held still when the user has asked for reduced motion.
      time: reduceMotion ? 0 : clock.value / 1000,
    }),
    [size, reduceMotion],
  );

  return (
    <View
      pointerEvents="none"
      style={[StyleSheet.absoluteFill, { backgroundColor: BASE[mode] }]}
      onLayout={(e) => setSize(e.nativeEvent.layout)}>
      {size.width > 0 && (
        <Canvas style={StyleSheet.absoluteFill}>
          <Fill>
            <Shader source={mode === 'day' ? day : night} uniforms={uniforms} />
          </Fill>
        </Canvas>
      )}
    </View>
  );
}
