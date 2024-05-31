local d = import 'github.com/jsonnet-libs/docsonnet/doc-util/main.libsonnet';

local panelUtil = import 'github.com/grafana/grafonnet/gen/grafonnet-v11.0.0/custom/util/panel.libsonnet';

// This is forked from https://grafana.github.io/grafonnet/API/util.html#obj-grid
// to allow automatic width to fill the grid
{
  local root = self,

  local gridWidth = 24,

  '#makeGrid':: d.func.new(
    |||
      `makeGrid` returns an array of `panels` organized in a grid with equal width
      and `panelHeight`. Row panels are used as "linebreaks", if a Row panel is collapsed,
      then all panels below it will be folded into the row.

      Optional `startY` can be provided to place generated grid above or below existing panels.
    |||,
    args=[
      d.arg('panels', d.T.array),
      d.arg('panelHeight', d.T.number),
      d.arg('startY', d.T.number),
    ],
  ),
  makeGrid(panels, panelHeight=8, startY=0):
    local sanitizePanels(ps, row) = std.map(
      function(p)
        local sanePanel = panelUtil.sanitizePanel(p);
        (
          if p.type == 'row'
          then sanePanel + {
            panels: sanitizePanels(sanePanel.panels, std.length(sanePanel.panels)),
          }
          else sanePanel + {
            gridPos+: {
              h: panelHeight,
              w: std.floor(gridWidth/row),
            },
          }
        ),
      ps
    );
    local sanitizedPanels = sanitizePanels(panels, 0);

    local grouped = panelUtil.groupPanelsInRows(sanitizedPanels);

    local panelsBeforeRows = panelUtil.getPanelsBeforeNextRow(grouped);
    local rowPanels =
      std.filter(
        function(p) p.type == 'row',
        grouped
      );

    local CalculateXforPanel(index, panel) =
      local panelsPerRow = std.floor(gridWidth / panel.gridPos.w);
      local col = std.mod(index, panelsPerRow);
      panel + { gridPos+: { x: panel.gridPos.w * col } };

    local panelsBeforeRowsWithX = std.mapWithIndex(CalculateXforPanel, panelsBeforeRows);

    local rowPanelsWithX =
      std.map(
        function(row)
          row + { panels: std.mapWithIndex(CalculateXforPanel, row.panels) },
        rowPanels
      );

    local uncollapsed = panelUtil.resolveCollapsedFlagOnRows(panelsBeforeRowsWithX + rowPanelsWithX);

    local normalized = panelUtil.normalizeY(uncollapsed);

    std.map(function(p) p + { gridPos+: { y+: startY } }, normalized),
}
