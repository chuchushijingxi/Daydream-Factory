import { useCatalogText } from '../../contexts/useCatalogText';
/** 星点引出线稿与分镜；纯展示，不读取项目或启动媒体任务。 */
export function AuthScene() {
  const { c } = useCatalogText();
  return (
    <aside className="auth-stage" aria-label={c('脑洞一开，好戏就来。')}>
      <svg className="auth-story" viewBox="0 0 640 420" fill="none" aria-hidden="true">
        <path className="story-moon" d="M151 45c-42-2-73 28-73 66 0 39 30 69 68 69 27 0 51-16 62-40-37 12-73-14-73-49 0-18 6-32 16-46Z" />
        <path className="story-trail" pathLength="1" d="M167 64c42-22 88-10 102 22 20 45-49 51-41 89" />
        <g className="story-sheet story-sheet-back story-mini">
          <rect x="77" y="220" width="190" height="129" rx="9" />
          <path d="M94 325h156M94 309l44-48 34 33 23-23 55 54" />
          <circle cx="222" cy="247" r="9" />
        </g>
        <g className="story-sheet story-sheet-far story-mini">
          <rect x="394" y="66" width="164" height="113" rx="9" />
          <path d="m412 149 30-34 24 21 27-40 47 53M412 160h128" />
        </g>
        <g className="story-sheet story-sheet-main">
          <rect x="196" y="147" width="339" height="220" rx="12" />
          <path className="story-land-fill" d="m216 326 68-83 55 57 51-38 124 64H216Z" />
          <path className="story-draw story-land" pathLength="1" d="m216 326 68-83 55 57 51-38 124 64M216 342h298M257 276l27 12 14-16" />
          <path className="story-corners" d="M210 175v-14h14m283 0h14v14M210 339v14h14m283 0h14v-14" />
        </g>
        <g className="story-spark">
          <path d="M0-17C3-6 6-3 17 0 6 3 3 6 0 17-3 6-6 3-17 0-6-3-3-6 0-17Z" />
        </g>
        <path className="story-ground" d="M87 385h483" />
      </svg>
      <div className="auth-stage-copy">
        <h2>
          {c('脑洞一开，')}
          <br />
          {c('好戏就来。')}
        </h2>
      </div>
    </aside>
  );
}
