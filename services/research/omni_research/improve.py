"""Public compatibility imports for deterministic SMA experiments."""

from .adapters.files.experiments import run_experiment
from .adapters.inputs.config import MINIMUM_FOLD_BARS, integer, parameter_values, parse_config
from .application.experiment import DISCLAIMER, EVALUATION_POLICY_VERSION, fold_description
from .domain.experiment import (
    Candidate, Evaluation, ExperimentConfig, IndexedTrade, Parameters, Span, ValidationFold,
    _evaluate, _simulate_indexed_fold, aggregate_validation, average, simulate_fold, sma_crossover,
)
