# Integracion nativa con tmux: informe de diseno

Veredicto: si, pero estrecha y en fase 3. Nunca como mecanismo principal,
siempre como acelerador opcional sobre un camino que funciona sin tmux.

La vista de logs agregada ES el producto (filtrar, colorear, buscar, scroll
unificado): no se delega en panes de tmux. tmux solo gana donde duck es
estructuralmente malo: hospedar un PTY interactivo.

## Prior art

- lazygit / lazydocker: cero tmux nativo; exec suspende la TUI. Extension via
  customCommands en config.
- k9s: nada nativo; plugins.yaml y plugins comunitarios que hacen
  `tmux split-window`.
- Ninguna TUI seria hardcodea tmux; todas exponen un punto de extension.

## Integraciones con mejor ratio valor/complejidad

### 1. Exec en pane/popup (el mejor ratio)

```sh
# split persistente
tmux split-window -h -t "$TMUX_PANE" -P -F '#{pane_id}' \
  -e DOCKER_HOST=ssh://host \
  -- docker exec -it <cid> sh -c 'command -v bash >/dev/null && exec bash || exec sh'

# popup modal (tmux >= 3.2)
tmux display-popup -E -w 80% -h 80% -T " exec: web " \
  -- docker exec -it <cid> sh -c '...'
```

- Anclar en `$TMUX_PANE`, no en el pane activo.
- Trampa multi-host: el pane hereda el entorno del shell del usuario, no el
  cliente in-process de duck. Propagar `DOCKER_HOST` (o `docker -c <ctx>`)
  siempre. Es el bug silencioso mas probable.

### 2. Explotar un stack en panes tiled (solo fire-and-forget)

```sh
WIN=$(tmux new-window -d -n "duck:<project>" -P -F '#{window_id}' \
      -- docker logs -f --tail=100 <cid-svc1>)
tmux split-window -d -t "$WIN" -P -F '#{pane_id}' -- docker logs -f --tail=100 <cid-svc2>
tmux set-option -p -t "$PANE" remain-on-exit on
tmux select-pane -t "$PANE" -T "<service>"
tmux select-layout -t "$WIN" tiled
tmux set-option -w -t "$WIN" pane-border-status top
```

- Usa `docker logs -f <cid>` por contenedor via labels compose: funciona sin
  fichero compose y sin plugin compose.
- Limite duro: duck no gestiona el ciclo de vida de esos panes. Sin
  reconciliacion, sin matar panes en stack down. Huerfanos: aceptar y
  documentar.

### 3. Rechazada: duck como orquestador de sesion tmux

Superficie enorme, estado sobre un recurso ajeno, compite con
sesh/tmuxinator. Ratio malo.

## Forma tecnica

Sistema de acciones externas configurables con presets tmux cuando `$TMUX`
esta presente. NO un interface Multiplexer por multiplexor (abstraccion
especulativa; zellij/wezterm/kitty se cubren con el mismo constructor de argv
mas config).

- El Model emite una intencion (`execRequestedMsg{container}`); una capa de
  comandos la convierte en `[]string` argv y la ejecuta.
- La pieza testeable es una funcion pura que construye el argv (table-driven,
  sin spawnear). Ejecucion detras de una interfaz minima de un metodo.
- Config con placeholders: `{{.ID}}`, `{{.Name}}`, `{{.Project}}`,
  `{{.Service}}`, `{{.DockerHost}}`.
- Deteccion: `$TMUX` no vacio y `exec.LookPath("tmux")`. Fallback:
  `tea.ExecProcess` (existe igualmente; la degradacion es gratis).

## Riesgos verificados

- Indices vs IDs: `base-index 1` en la config local rompe `-t sesion:0`.
  Nunca direccionar por indice; capturar `#{window_id}` / `#{pane_id}` con
  `-P -F` y direccionar por `@13` / `%21`.
- Escaping: usar `--` y argv como elementos separados. Jamas interpolar
  nombres de contenedor en un string de shell (vienen de labels, controlables
  por terceros en hosts compartidos).
- Gates de version (verificado sobre 3.7c): `-e` en split-window requiere
  3.0, `display-popup` 3.2, opciones de pane (`-p`) 3.0. Leer `tmux -V` al
  arrancar y degradar.
- Testing: nunca ejecutar tmux en tests unitarios; la costura es el
  constructor de argv.

## Fase

Fase 2 sin tocar tmux. Fase 3: primero `tea.ExecProcess` como camino base,
luego el preset tmux encima. Explotar stack despues, si el uso real lo pide.
