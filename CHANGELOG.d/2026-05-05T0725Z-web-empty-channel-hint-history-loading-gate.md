### Fixed

- Web: stopped the "This is the start of #&lt;channel&gt;" hint from flashing on
  channel switch. The empty-state copy now waits for the initial history
  fetch to settle, mirroring the existing no-channels guard (#579).
