//!HOOK ORIGINAL
//!BIND luma
//!SAVE sharpened
//!DESC soft-sharpen: unsharp mask (4-neighbour high boost)
//
// 引擎把每个 pass 都跑在源分辨率上，输出尺寸由包的 scale 决定，
// 所以这里绝不写 //!WIDTH / //!HEIGHT——带 2 * 的那类声明会被校验拒掉。
// 可用的只有 v_uv 与绑定的纹理：步长按 uv 的固定比例取，约合 1 个像素，
// 分辨率无关且不会依赖引擎没提供的 texel 尺寸宏。
vec4 hook() {
    vec2 ts = vec2(0.0008, 0.0012);
    vec4 centre = luma_tex(v_uv);
    vec4 up = luma_tex(v_uv + vec2(0.0, -ts.y));
    vec4 down = luma_tex(v_uv + vec2(0.0, ts.y));
    vec4 left = luma_tex(v_uv + vec2(-ts.x, 0.0));
    vec4 right = luma_tex(v_uv + vec2(ts.x, 0.0));

    vec4 blur = (up + down + left + right) * 0.25;
    vec4 detail = centre - blur;

    // 只放大高频的一部分，锐化过头会在胶片颗粒上起白边。
    vec4 res = clamp(centre + detail * 0.65, 0.0, 1.0);
    return res;
}
