from __future__ import annotations

import copy
from datetime import date, datetime, timedelta, timezone
from decimal import Decimal
import json
from pathlib import Path
import tempfile
import unittest

from omni_research.adapters.inputs.config import parse_config
from omni_research.adapters.inputs.proposals import generate_proposal_from_bytes
from omni_research.adapters.inputs.validation import request_execution
from omni_research.application.artifacts import canonical_hash
import test_signal_proposal as signal_fixtures

FIXTURES = Path(__file__).parents[3] / 'contracts' / 'fixtures'


class PolicyCompatibilityTest(unittest.TestCase):
    def test_execution_parses_all_numbers_before_range_and_delay_checks(self) -> None:
        execution = json.loads((FIXTURES / 'strategy-improvement-config.json').read_text())['execution']
        execution.update(starting_cash='0', tax='broken')
        with self.assertRaisesRegex(ValueError, 'execution.tax must be a decimal string'):
            request_execution({'execution': execution})
        execution.update(tax='0', delay_bars='0', max_participation='broken')
        with self.assertRaisesRegex(ValueError, 'execution.max_participation must be a decimal string'):
            request_execution({'execution': execution})
        execution['max_participation'] = '1'
        with self.assertRaisesRegex(ValueError, 'execution values are out of range'):
            request_execution({'execution': execution})

    def test_config_keeps_legacy_and_paper_quantity_window_limits_distinct(self) -> None:
        config = json.loads((FIXTURES / 'strategy-improvement-config.json').read_text())
        for quantity, slow in [('0.5', 366), ('9' * 64, 366)]:
            with self.subTest(quantity=quantity):
                config['strategy'].update(quantity=quantity, fast_windows=[2], slow_windows=[slow])
                self.assertEqual(parse_config(config).quantity, Decimal(quantity))
                paper = {**config, 'schema_version': 'strategy-improvement.v2'}
                with self.assertRaisesRegex(ValueError, 'paper evaluation requires'):
                    parse_config(paper)

    def test_proposal_rejection_does_not_read_later_missing_gate_fields(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            bars, research, original = signal_fixtures.SignalProposalTest().inputs(Path(temporary))
            for promotion in ({'target': 'no_promotion'},
                              {'target': 'paper_candidate', 'failed_gates': ['walk_forward']},
                              {'target': 'paper_candidate', 'failed_gates': [], 'walk_forward_gate_passed': 1}):
                artifact = copy.deepcopy(original)
                artifact['promotion'] = promotion
                artifact['result_sha256'] = canonical_hash({k: v for k, v in artifact.items() if k != 'result_sha256'})
                with self.subTest(promotion=promotion), self.assertRaisesRegex(ValueError, 'did not pass its paper gates'):
                    generate_proposal_from_bytes(bars.read_bytes(), research.read_bytes(), artifact)

    def test_proposal_keeps_missing_gate_and_invalid_windows_error_order(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            bars, research, original = signal_fixtures.SignalProposalTest().inputs(Path(temporary))
            artifact = copy.deepcopy(original)
            artifact['promotion'] = {'target': 'paper_candidate', 'failed_gates': []}
            artifact['result_sha256'] = canonical_hash({k: v for k, v in artifact.items() if k != 'result_sha256'})
            with self.assertRaisesRegex(KeyError, 'walk_forward_gate_passed'):
                generate_proposal_from_bytes(bars.read_bytes(), research.read_bytes(), artifact)
            artifact = copy.deepcopy(original)
            artifact['manifest']['strategy']['parameters']['fast_window'] = 3
            artifact['challenger'] = {}
            artifact['manifest']['strategy']['parameter_hash'] = canonical_hash(artifact['manifest']['strategy']['parameters'])
            artifact['result_sha256'] = canonical_hash({k: v for k, v in artifact.items() if k != 'result_sha256'})
            with self.assertRaisesRegex(ValueError, 'strategy windows are invalid'):
                generate_proposal_from_bytes(bars.read_bytes(), research.read_bytes(), artifact)

    def test_proposal_quantity_keeps_exact_canonical_integer_contract(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            bars, research, original = signal_fixtures.SignalProposalTest().inputs(Path(temporary))
            for quantity in ('0', '-0', '-1', '0.5', '1.5', '1.0', '01', '+1', '1e1', '9' * 65, 1, None):
                artifact = copy.deepcopy(original)
                strategy = artifact['manifest']['strategy']
                strategy['parameters']['quantity'] = quantity
                strategy['parameter_hash'] = canonical_hash(strategy['parameters'])
                artifact['result_sha256'] = canonical_hash({k: v for k, v in artifact.items() if k != 'result_sha256'})
                with self.subTest(quantity=quantity), self.assertRaisesRegex(ValueError, 'paper quantity must be positive whole shares'):
                    generate_proposal_from_bytes(bars.read_bytes(), research.read_bytes(), artifact)

    def test_legacy_proposal_allows_64_digit_quantity_and_slow_window_over_365(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            _, research, artifact = signal_fixtures.SignalProposalTest().inputs(Path(temporary))
            parameters = artifact['manifest']['strategy']['parameters']
            parameters.update(quantity='9' * 64, slow_window=366)
            artifact['challenger']['parameters']['slow_window'] = 366
            artifact['manifest']['strategy']['parameter_hash'] = canonical_hash(parameters)
            artifact['result_sha256'] = canonical_hash({k: v for k, v in artifact.items() if k != 'result_sha256'})
            start = datetime(2026, 5, 1, tzinfo=timezone.utc)
            raw = ('bar_at,symbol,open,high,low,close,volume\n' + ''.join(
                f'{(start + timedelta(days=i)).strftime("%Y-%m-%dT%H:%M:%SZ")},005930,1,1,1,1,100\n'
                for i in range(367))).encode()
            self.assertEqual(generate_proposal_from_bytes(raw, research.read_bytes(), artifact)['signal'], 'none')


class DomainPolicyTest(unittest.TestCase):
    def test_promotion_requires_every_gate_and_keeps_failed_gate_order(self) -> None:
        from omni_research.domain.experiment import promotion_outcome
        self.assertEqual(promotion_outcome(True, True, True), ('paper_candidate', []))
        self.assertEqual(promotion_outcome(False, False, False),
                         ('no_promotion', ['walk_forward', 'final_holdout', 'buy_and_hold_baseline']))
        for gates, failed in [((False, True, True), ['walk_forward']),
                              ((True, False, True), ['final_holdout']),
                              ((True, True, False), ['buy_and_hold_baseline'])]:
            self.assertEqual(promotion_outcome(*gates), ('no_promotion', failed))

    def test_sma_signal_target_distinguishes_exit_and_no_change(self) -> None:
        from omni_research.domain.experiment import signal_target
        self.assertEqual(signal_target('golden_cross', '12'), '12')
        self.assertEqual(signal_target('death_cross', '12'), '0')
        self.assertIsNone(signal_target('none', '12'))

    def test_proposal_quantity_has_no_paper_experiment_cap(self) -> None:
        from omni_research.domain.experiment import validate_proposal_quantity
        for value in ('1', '9' * 64):
            validate_proposal_quantity(Decimal(value))
        for value in ('0', '-0', '-1', '0.5', '1.5'):
            with self.subTest(value=value), self.assertRaisesRegex(ValueError, 'paper quantity must be positive whole shares'):
                validate_proposal_quantity(Decimal(value))

    def test_execution_policy_boundaries_and_lookahead(self) -> None:
        from omni_research.domain.backtest import validate_execution_values
        valid = [Decimal('1'), Decimal('0'), Decimal('0'), Decimal('0'), Decimal('1'), Decimal('1')]
        validate_execution_values(*valid)
        validate_execution_values(Decimal('1'), Decimal('0'), Decimal('1'), Decimal('9999.99'), Decimal('2'), Decimal('0.01'))
        for index, value in [(0, '0'), (1, '-1'), (2, '-0.1'), (2, '1.01'), (3, '-1'), (3, '10000'), (5, '0'), (5, '1.01')]:
            values = valid.copy()
            values[index] = Decimal(value)
            with self.subTest(index=index, value=value), self.assertRaisesRegex(ValueError, 'values are out of range'):
                validate_execution_values(*values)
        for delay in ('0', '0.5', '1.5'):
            values = valid.copy()
            values[4] = Decimal(delay)
            with self.subTest(delay=delay), self.assertRaisesRegex(ValueError, 'prevent lookahead'):
                validate_execution_values(*values)

    def test_experiment_grid_fold_and_drawdown_boundaries(self) -> None:
        from omni_research.domain.experiment import Parameters, validate_grid, validate_validation_folds, validate_drawdown
        validate_grid(tuple(Parameters(fast, 100) for fast in range(1, 65)))
        for candidates in (tuple(Parameters(fast, 100) for fast in range(1, 66)), (Parameters(2, 2),)):
            with self.assertRaisesRegex(ValueError, 'strategy grid must contain at most 64'):
                validate_grid(candidates)
        validate_validation_folds(60, 30)
        for count in (30, 61):
            with self.assertRaisesRegex(ValueError, 'at least two complete walk-forward folds'):
                validate_validation_folds(count, 30)
        for value in ('0', '1'):
            validate_drawdown(Decimal(value))
        for value in ('-0.1', '1.1'):
            with self.assertRaisesRegex(ValueError, 'must be in'):
                validate_drawdown(Decimal(value))

    def test_request_business_admissibility(self) -> None:
        from omni_research.domain import backtest
        first, second = datetime(2026, 5, 1), datetime(2026, 5, 2)
        backtest.validate_run_times(second, first)
        backtest.validate_run_times(first, first)
        with self.assertRaisesRegex(ValueError, 'finished_at must not precede'):
            backtest.validate_run_times(first, second)
        backtest.validate_strategy('buy_and_hold', True)
        for name, liquidate in [('other', True), ('buy_and_hold', False), ('buy_and_hold', 1)]:
            with self.assertRaisesRegex(ValueError, 'only buy_and_hold'):
                backtest.validate_strategy(name, liquidate)
        backtest.validate_daily_input('1d', 'UTC')
        for interval, zone in [('1h', 'UTC'), ('1d', 'Asia/Seoul')]:
            with self.assertRaisesRegex(ValueError, 'only UTC daily bars'):
                backtest.validate_daily_input(interval, zone)
        backtest.validate_quantity(Decimal('0.5'))
        with self.assertRaisesRegex(ValueError, 'strategy parameters or parameter_hash are invalid'):
            backtest.validate_quantity(Decimal('0'))

    def test_domain_minimums_and_window_normalization(self) -> None:
        from omni_research.domain.experiment import MINIMUM_FOLD_BARS, MINIMUM_SMA_WINDOW, MINIMUM_TRADE_COUNT, parameter_windows
        from omni_research.domain.values import minimum_integer
        for field, minimum in [('fold', MINIMUM_FOLD_BARS), ('window', MINIMUM_SMA_WINDOW), ('trades', MINIMUM_TRADE_COUNT)]:
            self.assertEqual(minimum_integer(minimum, field, minimum), minimum)
            with self.assertRaisesRegex(ValueError, f'{field} must be an integer of at least {minimum}'):
                minimum_integer(minimum - 1, field, minimum)
        self.assertEqual(parameter_windows((3, 1, 2), 'fast'), (1, 2, 3))
        with self.assertRaisesRegex(ValueError, 'fast must not contain duplicates'):
            parameter_windows((1, 1), 'fast')

    def test_market_values_and_observation_time_admissibility(self) -> None:
        from omni_research.domain.values import Bar, valid_ohlcv, validate_bars, validate_bar_times, validate_observation_day, validate_fetch_order, validate_signal_time
        bar = Bar('2026-05-01', '005930', *(Decimal(v) for v in ('2', '3', '1', '2', '0')))
        self.assertTrue(valid_ohlcv(bar))
        validate_bars([bar])
        from dataclasses import replace
        for bars in ([], [bar, replace(bar, symbol='000660')]):
            with self.assertRaisesRegex(ValueError, 'exactly one symbol'):
                validate_bars(bars)
        with self.assertRaisesRegex(ValueError, 'bars require a symbol'):
            validate_bars([replace(bar, symbol='')])
        for changes in ({'open': Decimal('0')}, {'volume': Decimal('-1')}, {'low': Decimal('3')}, {'high': Decimal('1')}):
            self.assertFalse(valid_ohlcv(replace(bar, **changes)))
        first, second = datetime(2026, 5, 1), datetime(2026, 5, 2)
        validate_bar_times([first, second])
        validate_signal_time(second, first)
        for signal_time in (first, first - timedelta(days=1)):
            with self.assertRaisesRegex(ValueError, 'signal must follow the research sample'):
                validate_signal_time(signal_time, first)
        for times in ([second, first], [first, first]):
            with self.assertRaisesRegex(ValueError, 'strictly ordered'):
                validate_bar_times(times)
        validate_fetch_order((first, 1), (first, 2))
        with self.assertRaisesRegex(ValueError, 'fetch clock order is reversed'):
            validate_fetch_order((first, 2), (first, 1))
        validate_observation_day(date(2026, 5, 1), None, date(2026, 5, 1))
        for previous, base in ((date(2026, 5, 1), date(2026, 5, 2)), (None, date(2026, 4, 30))):
            with self.assertRaisesRegex(ValueError, 'unique, increasing and no later'):
                validate_observation_day(date(2026, 5, 1), previous, base)


if __name__ == '__main__':
    unittest.main()
