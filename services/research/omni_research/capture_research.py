"""Public compatibility imports for native capture research."""

from .adapters.files.experiments import run_capture_experiment
from .adapters.files.inputs import read_capture_input as _read_input
from .adapters.inputs.capture import _capture_bars, _capture_config
from .adapters.inputs.json_values import _decode
from .application.capture import LIMITATIONS, MAX_BYTES
