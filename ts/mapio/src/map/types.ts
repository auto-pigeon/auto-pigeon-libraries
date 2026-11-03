export type SourceRange={line:number;column:number;excerpt:string}; export type MapParseError={code:string;message:string;range:SourceRange;entityIndex?:number;brushIndex?:number};
/**
 * `brush_primitives` (`Q3_002`): a Quake III brushDef face. `texture_axes` are its matrix read as world covectors in
 * TEXTURE units (`geometry/brushPrimitives.ts`), resolved once at decode from the face's own plane points because
 * the matrix's axis base depends on the sign of Q3Map2's normal. `shift`, `rotation` and `scale` are the identity
 * and mean nothing for this mode; `matrix` is the document's, verbatim, for the Inspector.
 */
export type FaceMapping={mode:'classic'|'valve220'|'brush_primitives';shift:{x:number;y:number};rotation:number;scale:{x:number;y:number};valve_220_axes?:{u:{x:number;y:number;z:number;shift:number};v:{x:number;y:number;z:number;shift:number}};texture_axes?:{u:{x:number;y:number;z:number;shift:number};v:{x:number;y:number;z:number;shift:number}};matrix?:[[number,number,number],[number,number,number]];
  /** `NEW_325B`: present (`'quake3'`) only on a Classic face of a Quake III document — which compiler's base-axis rule reads it (`classicAxes`). Absent means `qbsp`'s. */
  classic_reader?:'quake'|'quake3'};
/** `stableId` is present only when the source document carries object identity of its own (APMap `ent_`/`brs_`/`fac_` ids). A `.map` has none, so the worker falls back to positional ids. */
export type ParsedFace={index:number;stableId?:string;points:[[number,number,number],[number,number,number],[number,number,number]];textureName:string;mapping:FaceMapping;range:SourceRange;tail:string[]};
export type ParsedBrush={index:number;stableId?:string;faces:ParsedFace[];range:SourceRange}; /**
 * One `"key" "value"` line, in source order — `Q2_002`. `properties` is the engine's reading (a later
 * repeat wins), which is what every renderer and form asks; `entries` is the SOURCE's, which is what a
 * lossless re-encoding needs: a repeated key keeps both copies, and `afterBrushes` (how many of the
 * entity's brushes preceded the line) keeps a key written after a brush after it.
 */
export type ParsedProperty={key:string;value:string;afterBrushes:number};
export type ParsedEntity={index:number;stableId?:string;properties:Record<string,string>;
  /** Source order and repeats (`Q2_002`). Absent on a model built by a reader that keeps none; then `properties` is the order. */
  entries?:ParsedProperty[];brushes:ParsedBrush[];range:SourceRange}; export type ParsedMap={entities:ParsedEntity[];errors:MapParseError[]};
