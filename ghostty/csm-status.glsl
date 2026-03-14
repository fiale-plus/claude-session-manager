// CSM Status Shader — Claude Session Manager
// A subtle top-edge accent line that pulses gently, giving a visual hint
// that CSM is active. Purely aesthetic — does not convey session state
// (shaders cannot read external data).
//
// Install: custom-shader = ~/.config/ghostty/shaders/csm-status.glsl
//          custom-shader-animation = true

void mainImage(out vec4 fragColor, in vec2 fragCoord) {
    vec2 uv = fragCoord.xy / iResolution.xy;

    // Sample the terminal content.
    vec4 terminal = texture(iChannel0, uv);

    // ── Top accent line (2px) ───────────────────────────────────
    float lineThickness = 2.0 / iResolution.y;
    float topEdge = 1.0 - uv.y;

    if (topEdge < lineThickness) {
        // Indigo base: #6366f1 → rgb(0.388, 0.400, 0.945)
        vec3 indigo = vec3(0.388, 0.400, 0.945);
        // Subtle purple shift: #8b5cf6 → rgb(0.545, 0.361, 0.965)
        vec3 purple = vec3(0.545, 0.361, 0.965);

        // Slow horizontal gradient oscillation.
        float gradPos = uv.x + sin(iTime * 0.5) * 0.15;
        vec3 lineColor = mix(indigo, purple, gradPos);

        // Gentle pulse (90-100% opacity).
        float pulse = 0.9 + 0.1 * sin(iTime * 1.5);

        // Soft edge falloff.
        float edgeFade = smoothstep(0.0, lineThickness, topEdge);
        float alpha = pulse * (1.0 - edgeFade);

        fragColor = vec4(mix(terminal.rgb, lineColor, alpha), terminal.a);
        return;
    }

    // ── Corner dots (bottom-left status indicator) ──────────────
    float dotRadius = 3.0 / iResolution.y;
    float dotMargin = 8.0 / iResolution.y;
    float dotSpacing = 10.0 / iResolution.x;

    // Three small dots in bottom-left corner.
    for (int i = 0; i < 3; i++) {
        vec2 dotCenter = vec2(
            dotMargin + float(i) * dotSpacing,
            dotMargin
        );
        float dist = distance(uv, dotCenter);
        if (dist < dotRadius) {
            // Staggered pulse for each dot.
            float dotPulse = 0.4 + 0.6 * sin(iTime * 2.0 + float(i) * 1.2);
            vec3 dotColor = vec3(0.388, 0.400, 0.945); // indigo
            float dotAlpha = dotPulse * smoothstep(dotRadius, dotRadius * 0.3, dist);
            fragColor = vec4(mix(terminal.rgb, dotColor, dotAlpha * 0.7), terminal.a);
            return;
        }
    }

    // Pass through terminal content unchanged.
    fragColor = terminal;
}
