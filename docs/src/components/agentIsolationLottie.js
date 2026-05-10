const WIDTH = 1200;
const HEIGHT = 420;
const DURATION = 240;

const defaultColors = {
  terminal: [0.025, 0.034, 0.07, 1],
  terminalTop: [0.055, 0.073, 0.12, 1],
  terminalLine: [0.23, 0.3, 0.42, 1],
  leftTerminal: [0.02, 0.027, 0.055, 0.9],
  rightTerminal: [0.018, 0.04, 0.036, 0.9],
  leftBorder: [0.58, 0.67, 0.9, 0.8],
  rightBorder: [0.42, 0.95, 0.68, 0.7],
  dangerLine: [0.32, 0.14, 0.16, 1],
  dangerLineMuted: [0.35, 0.18, 0.18, 1],
  sharedBus: [0.18, 0.22, 0.34, 1],
  inactiveLight: [0.32, 0.38, 0.48, 1],
  sandboxFill: [0.03, 0.09, 0.075, 0.42],
  sandboxBorder: [0.4, 0.95, 0.72, 0.95],
  rightStatus: [0.18, 0.38, 0.28, 1],
  popupPanel: [0.09, 0.055, 0.06, 0.98],
  popupShadow: [0, 0, 0, 0.34],
  cursorBody: [0.92, 0.97, 1, 1],
  cursorOutline: [0.02, 0.03, 0.05, 1],
  cursorShadow: [0, 0, 0, 1],
  cyan: [0.22, 0.86, 1, 1],
  green: [0.34, 0.95, 0.64, 1],
  purple: [0.72, 0.55, 1, 1],
  amber: [1, 0.7, 0.25, 1],
  red: [1, 0.32, 0.32, 1],
  text: [0.86, 0.92, 1, 1],
  mutedText: [0.5, 0.62, 0.77, 1],
  darkText: [0.08, 0.1, 0.16, 1],
};

let colors = defaultColors;

const LEFT_OFFSET = 80;
const RIGHT_OFFSET = -80;

const leftX = (x) => x + LEFT_OFFSET;
const rightX = (x) => x + RIGHT_OFFSET;
const shiftX = (points, offset) => points.map(([frame, x, y]) => [frame, x + offset, y]);

let layerIndex = 1;

const prop = (value) => ({ a: 0, k: value });

const ease = {
  i: { x: 0.65, y: 1 },
  o: { x: 0.35, y: 0 },
};

function keyframes(points, mapValue) {
  return points.map((point, index) => {
    const next = points[index + 1];
    const frame = {
      t: point[0],
      s: mapValue(point),
      ...ease,
    };

    if (next) {
      frame.e = mapValue(next);
    }

    return frame;
  });
}

function scalarFrames(points) {
  return { a: 1, k: keyframes(points, ([, value]) => [value]) };
}

function positionFrames(points, offset = [0, 0]) {
  return {
    a: 1,
    k: keyframes(points, ([, x, y]) => [x + offset[0], y + offset[1], 0]),
  };
}

function transform({
  opacity = 100,
  position = [0, 0, 0],
  anchor = [0, 0, 0],
  scale = [100, 100, 100],
  rotation = 0,
} = {}) {
  return {
    o: typeof opacity === "number" ? prop(opacity) : opacity,
    r: prop(rotation),
    p: Array.isArray(position) ? prop(position) : position,
    a: prop(anchor),
    s: Array.isArray(scale) ? prop(scale) : scale,
  };
}

function shapeLayer(name, shapes, options = {}) {
  return {
    ddd: 0,
    ind: layerIndex++,
    ty: 4,
    nm: name,
    sr: 1,
    ks: transform(options),
    ao: 0,
    shapes,
    ip: 0,
    op: DURATION,
    st: 0,
    bm: 0,
  };
}

function textLayer(text, x, y, size, color = colors.text, options = {}) {
  return {
    ddd: 0,
    ind: layerIndex++,
    ty: 5,
    nm: options.name ?? text,
    sr: 1,
    ks: transform({
      opacity: options.opacity ?? 100,
      position: options.position ?? [x, y, 0],
      anchor: [0, 0, 0],
    }),
    ao: 0,
    t: {
      d: {
        k: [
          {
            s: {
              sz: options.box ?? [360, 42],
              ps: [0, 0],
              s: size,
              f: "GeistMono-Regular",
              t: text,
              j: options.align ?? 0,
              tr: 0,
              lh: size * 1.25,
              ls: 0,
              fc: color.slice(0, 3),
            },
            t: 0,
          },
        ],
      },
      p: {},
      m: { g: 1, a: prop([0, 0]) },
      a: [],
    },
    ip: 0,
    op: DURATION,
    st: 0,
    bm: 0,
  };
}

function group(name, items, transformOptions = {}) {
  return {
    ty: "gr",
    nm: name,
    it: [
      ...items,
      {
        ty: "tr",
        p: prop(transformOptions.position ?? [0, 0]),
        a: prop([0, 0]),
        s: prop(transformOptions.scale ?? [100, 100]),
        r: prop(transformOptions.rotation ?? 0),
        o: prop(transformOptions.opacity ?? 100),
        sk: prop(0),
        sa: prop(0),
      },
    ],
  };
}

function rect(name, x, y, width, height, radius, fill, stroke, strokeWidth = 2) {
  const items = [
    {
      ty: "rc",
      nm: `${name} path`,
      d: 1,
      s: prop([width, height]),
      p: prop([x, y]),
      r: prop(radius),
    },
  ];

  if (fill) {
    items.push({
      ty: "fl",
      nm: `${name} fill`,
      c: prop(fill),
      o: prop(fill[3] * 100),
      r: 1,
      bm: 0,
    });
  }

  if (stroke) {
    items.push({
      ty: "st",
      nm: `${name} stroke`,
      c: prop(stroke),
      o: prop(stroke[3] * 100),
      w: prop(strokeWidth),
      lc: 2,
      lj: 2,
      ml: 4,
      bm: 0,
    });
  }

  return group(name, items);
}

function dashedRect(name, x, y, width, height, radius, stroke, strokeWidth = 2) {
  const items = [];
  const dash = 22;
  const gap = 14;
  const halfWidth = width / 2;
  const halfHeight = height / 2;
  const horizontalStart = -halfWidth + radius;
  const horizontalEnd = halfWidth - radius;
  const verticalStart = -halfHeight + radius;
  const verticalEnd = halfHeight - radius;

  for (let offset = horizontalStart; offset < horizontalEnd; offset += dash + gap) {
    const length = Math.min(dash, horizontalEnd - offset);
    const center = offset + length / 2;

    items.push(
      {
        ty: "rc",
        nm: `${name} top dash`,
        d: 1,
        s: prop([length, strokeWidth]),
        p: prop([center, -halfHeight]),
        r: prop(strokeWidth / 2),
      },
      {
        ty: "rc",
        nm: `${name} bottom dash`,
        d: 1,
        s: prop([length, strokeWidth]),
        p: prop([center, halfHeight]),
        r: prop(strokeWidth / 2),
      },
    );
  }

  for (let offset = verticalStart; offset < verticalEnd; offset += dash + gap) {
    const length = Math.min(dash, verticalEnd - offset);
    const center = offset + length / 2;

    items.push(
      {
        ty: "rc",
        nm: `${name} left dash`,
        d: 1,
        s: prop([strokeWidth, length]),
        p: prop([-halfWidth, center]),
        r: prop(strokeWidth / 2),
      },
      {
        ty: "rc",
        nm: `${name} right dash`,
        d: 1,
        s: prop([strokeWidth, length]),
        p: prop([halfWidth, center]),
        r: prop(strokeWidth / 2),
      },
    );
  }

  items.push({
    ty: "fl",
    nm: `${name} dash fill`,
    c: prop(stroke),
    o: prop(stroke[3] * 100),
    r: 1,
    bm: 0,
  });

  return group(name, items, { position: [x, y] });
}

function line(name, x, y, width, color, opacity = 100) {
  return group(name, [
    {
      ty: "rc",
      nm: `${name} path`,
      d: 1,
      s: prop([width, 4]),
      p: prop([x, y]),
      r: prop(2),
    },
    {
      ty: "fl",
      nm: `${name} fill`,
      c: prop(color),
      o: prop(opacity),
      r: 1,
      bm: 0,
    },
  ]);
}

function circle(name, x, y, radius, fill) {
  return group(name, [
    {
      ty: "el",
      nm: `${name} path`,
      d: 1,
      s: prop([radius * 2, radius * 2]),
      p: prop([x, y]),
    },
    {
      ty: "fl",
      nm: `${name} fill`,
      c: prop(fill),
      o: prop(fill[3] * 100),
      r: 1,
      bm: 0,
    },
  ]);
}

function path(name, vertices, inTangents, outTangents, closed, stroke, strokeWidth = 4) {
  return group(name, [
    {
      ty: "sh",
      nm: `${name} path`,
      ks: prop({ i: inTangents, o: outTangents, v: vertices, c: closed }),
    },
    {
      ty: "st",
      nm: `${name} stroke`,
      c: prop(stroke),
      o: prop(stroke[3] * 100),
      w: prop(strokeWidth),
      lc: 2,
      lj: 2,
      ml: 4,
      bm: 0,
    },
  ]);
}

function cursorPointerPath(name) {
  return {
    ty: "sh",
    nm: `${name} pointer path`,
    ks: prop({
      i: [
        [0, 0],
        [0, 0],
        [0, 0],
        [-1.1, 0.4],
        [0, 0],
        [-0.4, -1.1],
        [0, 0],
      ],
      o: [
        [0, 0],
        [0, 0],
        [0.7, 1],
        [0, 0],
        [1.1, -0.4],
        [0, 0],
        [0, 0],
      ],
      v: [
        [0, 0],
        [2, 47],
        [14, 35],
        [22, 54],
        [33, 50],
        [25, 31],
        [43, 31],
      ],
      c: true,
    }),
  };
}

function cursorLayer(name, color, frames, opacity = 100) {
  return shapeLayer(
    `${name} cursor`,
    [
      group(`${name} cursor shape`, [
        group(`${name} pointer shadow`, [
          cursorPointerPath(`${name} shadow`),
          {
            ty: "fl",
            nm: `${name} shadow fill`,
            c: prop(colors.cursorShadow),
            o: prop(36),
            r: 1,
            bm: 0,
          },
        ], { position: [3, 4] }),
        cursorPointerPath(`${name} color edge`),
        {
          ty: "st",
          nm: `${name} color edge stroke`,
          c: prop(color),
          o: prop(92),
          w: prop(5.5),
          lc: 2,
          lj: 2,
          ml: 4,
          bm: 0,
        },
        cursorPointerPath(`${name} body`),
        {
          ty: "fl",
          nm: `${name} body fill`,
          c: prop(colors.cursorBody),
          o: prop(100),
          r: 1,
          bm: 0,
        },
        cursorPointerPath(`${name} body outline`),
        {
          ty: "st",
          nm: `${name} body outline stroke`,
          c: prop(colors.cursorOutline),
          o: prop(96),
          w: prop(2.4),
          lc: 2,
          lj: 2,
          ml: 4,
          bm: 0,
        },
        path(
          `${name} inner bevel`,
          [
            [7, 13],
            [8, 36],
            [14, 30],
          ],
          [
            [0, 0],
            [0, 0],
            [0, 0],
          ],
          [
            [0, 0],
            [0, 0],
            [0, 0],
          ],
          false,
          color,
          1.7,
        ),
      ], { scale: [56, 56] }),
    ],
    {
      opacity,
      position: positionFrames(frames),
    },
  );
}

function popup(prefix, message, x, y, width, color, visibleFrames) {
  const opacity = scalarFrames([
    [0, 0],
    [visibleFrames[0] - 8, 0],
    [visibleFrames[0], 100],
    [visibleFrames[1], 100],
    [visibleFrames[1] + 12, 0],
    [DURATION, 0],
  ]);

  const scale = {
    a: 1,
    k: keyframes(
      [
        [0, 96],
        [visibleFrames[0] - 8, 96],
        [visibleFrames[0], 100],
        [visibleFrames[1], 100],
        [visibleFrames[1] + 12, 97],
        [DURATION, 97],
      ],
      ([, value]) => [value, value, 100],
    ),
  };

  const panel = shapeLayer(
      `${prefix} popup`,
      [
        rect(`${prefix} popup shadow`, 4, 6, width, 58, 0, colors.popupShadow),
        rect(`${prefix} popup panel`, 0, 0, width, 58, 0, colors.popupPanel, color, 2),
        circle(`${prefix} warn dot`, -width / 2 + 22, -1, 6, color),
      ],
      {
        opacity,
        position: [x, y, 0],
        scale,
      },
    );

  return [
    textLayer(message, x - width / 2 + 40, y - 10, 17, colors.text, {
      name: `${prefix} message`,
      box: [prefix === "database" ? width - 96 : width - 58, 28],
      opacity,
    }),
    textLayer("x", x + width / 2 - 26, y - 11, 17, color, {
      name: `${prefix} close`,
      box: [20, 28],
      opacity,
    }),
    panel,
  ];
}

function desktopBaseShapes() {
  return [
    rect("left terminal", leftX(255), 210, 500, 310, 0, colors.leftTerminal, colors.leftBorder, 2),
    rect("right terminal", rightX(945), 210, 500, 310, 0, colors.rightTerminal, colors.rightBorder, 2),
  ];
}

function desktopDetailShapes() {
  return [
    rect("left terminal outline", leftX(255), 210, 500, 310, 0, null, colors.leftBorder, 2.4),
    rect("left terminal header", leftX(255), 78, 500, 46, 0, colors.terminalTop),
    circle("left red light", leftX(67), 78, 5, colors.red),
    circle("left amber light", leftX(85), 78, 5, colors.amber),
    circle("left green light", leftX(103), 78, 5, colors.green),
    line("left prompt line 1", leftX(128), 122, 116, colors.terminalLine, 44),
    line("left prompt line 2", leftX(191), 146, 192, colors.terminalLine, 34),
    line("left prompt line 3", leftX(157), 174, 132, colors.terminalLine, 29),
    line("left prompt line 4", leftX(220), 308, 270, colors.dangerLine, 38),
    line("left prompt line 5", leftX(165), 335, 160, colors.dangerLineMuted, 36),
    line("left shared bus", leftX(256), 235, 330, colors.sharedBus, 42),
    rect("right terminal outline", rightX(945), 210, 500, 310, 0, null, colors.rightBorder, 2.4),
    rect("right terminal header", rightX(945), 78, 500, 46, 0, colors.terminalTop),
    circle("right red light", rightX(757), 78, 5, colors.inactiveLight),
    circle("right amber light", rightX(775), 78, 5, colors.inactiveLight),
    circle("right green light", rightX(793), 78, 5, colors.green),
    rect("sandbox agent 1 fill", rightX(855), 155, 160, 96, 16, colors.sandboxFill),
    rect("sandbox agent 2 fill", rightX(1035), 155, 160, 96, 16, colors.sandboxFill),
    rect("sandbox agent 3 fill", rightX(855), 285, 160, 96, 16, colors.sandboxFill),
    rect("sandbox agent 4 fill", rightX(1035), 285, 160, 96, 16, colors.sandboxFill),
    dashedRect("sandbox agent 1", rightX(855), 155, 160, 96, 16, colors.sandboxBorder, 2),
    dashedRect("sandbox agent 2", rightX(1035), 155, 160, 96, 16, colors.sandboxBorder, 2),
    dashedRect("sandbox agent 3", rightX(855), 285, 160, 96, 16, colors.sandboxBorder, 2),
    dashedRect("sandbox agent 4", rightX(1035), 285, 160, 96, 16, colors.sandboxBorder, 2),
    line("sandbox 1 code", rightX(857), 185, 74, colors.terminalLine, 50),
    line("sandbox 2 code", rightX(1037), 185, 74, colors.terminalLine, 50),
    line("sandbox 3 code", rightX(857), 315, 74, colors.terminalLine, 50),
    line("sandbox 4 code", rightX(1037), 315, 74, colors.terminalLine, 50),
    line("right status line", rightX(945), 335, 260, colors.rightStatus, 36),
  ];
}

function addAgentLayers(layers) {
  const leftAgent1 = shiftX([
    [0, 105, 115],
    [48, 188, 152],
    [92, 122, 245],
    [150, 310, 150],
    [220, 105, 115],
    [240, 105, 115],
  ], LEFT_OFFSET);
  const leftAgent2 = shiftX([
    [0, 374, 103],
    [56, 306, 205],
    [112, 390, 258],
    [170, 230, 134],
    [240, 374, 103],
  ], LEFT_OFFSET);
  const leftAgent3 = shiftX([
    [0, 126, 274],
    [54, 250, 252],
    [100, 172, 148],
    [160, 356, 304],
    [240, 126, 274],
  ], LEFT_OFFSET);
  const leftAgent4 = shiftX([
    [0, 274, 324],
    [66, 354, 176],
    [118, 180, 318],
    [178, 410, 226],
    [240, 274, 324],
  ], LEFT_OFFSET);

  const rightAgent1 = shiftX([
    [0, 812, 130],
    [45, 884, 126],
    [100, 902, 155],
    [155, 850, 171],
    [205, 800, 148],
    [240, 812, 130],
  ], RIGHT_OFFSET);
  const rightAgent2 = shiftX([
    [0, 1080, 132],
    [50, 1010, 128],
    [110, 972, 150],
    [168, 1005, 172],
    [215, 1075, 162],
    [240, 1080, 132],
  ], RIGHT_OFFSET);
  const rightAgent3 = shiftX([
    [0, 895, 270],
    [42, 842, 250],
    [86, 796, 276],
    [132, 820, 302],
    [184, 908, 294],
    [240, 895, 270],
  ], RIGHT_OFFSET);
  const rightAgent4 = shiftX([
    [0, 972, 300],
    [50, 1046, 298],
    [100, 1082, 258],
    [150, 1012, 246],
    [205, 966, 278],
    [240, 972, 300],
  ], RIGHT_OFFSET);

  layers.push(cursorLayer("Agent-1", colors.cyan, leftAgent1));
  layers.push(cursorLayer("Agent-2", colors.purple, leftAgent2));
  layers.push(cursorLayer("Agent-3", colors.amber, leftAgent3));
  layers.push(cursorLayer("Agent-4", colors.green, leftAgent4));

  layers.push(cursorLayer("Agent-1 isolated", colors.cyan, rightAgent1, 96));
  layers.push(cursorLayer("Agent-2 isolated", colors.purple, rightAgent2, 96));
  layers.push(cursorLayer("Agent-3 isolated", colors.amber, rightAgent3, 96));
  layers.push(cursorLayer("Agent-4 isolated", colors.green, rightAgent4, 96));
}

export function createAgentIsolationAnimationData(palette = {}) {
  layerIndex = 1;
  colors = { ...defaultColors, ...palette };
  const layers = [];

  layers.push(textLayer("shared desktop", leftX(128), 74, 15, colors.mutedText, { box: [180, 24] }));
  layers.push(textLayer("isolated environments", rightX(818), 74, 15, colors.mutedText, { box: [240, 24] }));
  layers.push(textLayer("vm-01", rightX(802), 114, 14, colors.green, { box: [80, 24] }));
  layers.push(textLayer("vm-02", rightX(982), 114, 14, colors.green, { box: [80, 24] }));
  layers.push(textLayer("vm-03", rightX(802), 244, 14, colors.green, { box: [80, 24] }));
  layers.push(textLayer("vm-04", rightX(982), 244, 14, colors.green, { box: [80, 24] }));

  layers.push(...popup("database", "database dropped", leftX(218), 195, 270, colors.red, [28, 92]));
  layers.push(...popup("file", "file deleted", leftX(300), 252, 206, colors.amber, [68, 132]));
  layers.push(...popup("merge", "data leaked", leftX(375), 305, 218, colors.red, [104, 178]));

  addAgentLayers(layers);

  layers.push(shapeLayer("desktop details", desktopDetailShapes()));
  layers.push(shapeLayer("desktop bases", desktopBaseShapes()));

  return {
    v: "5.13.0",
    fr: 60,
    ip: 0,
    op: DURATION,
    w: WIDTH,
    h: HEIGHT,
    nm: "Bastion agent isolation hero",
    ddd: 0,
    assets: [],
    fonts: {
      list: [
        {
          fName: "GeistMono-Regular",
          fFamily: "Geist Mono",
          fStyle: "Regular",
          ascent: 75,
        },
      ],
    },
    layers,
    markers: [],
  };
}
