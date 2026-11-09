import 'package:flutter/material.dart';
import 'package:omni_folio_client/app/client_dependencies.dart';
import 'package:omni_folio_client/core/presentation/components.dart';
import 'package:omni_folio_client/features/connections/presentation/connection_cards.dart';
import 'package:omni_folio_client/features/market_data/application/owner_data_import_controller.dart';
import 'package:omni_folio_client/features/market_data/presentation/owner_data_import_page.dart';
import 'package:omni_folio_client/features/paper/presentation/paper_monitor_page.dart';
import 'package:omni_folio_client/features/paper/presentation/paper_promotion_report_page.dart';
import 'package:omni_folio_client/features/portfolio/application/portfolio_controller.dart';
import 'package:omni_folio_client/features/research/presentation/public_research_page.dart';
import 'package:omni_folio_client/features/research/presentation/research_report_page.dart';

class DataPage extends StatelessWidget {
  const DataPage({
    super.key,
    required this.dependencies,
    required this.controller,
    this.ownerImports,
  });
  final ClientDependencies dependencies;
  final PortfolioController controller;
  final OwnerDataImportController? ownerImports;

  @override
  Widget build(BuildContext context) {
    final baseUrl = dependencies.connectionLabel;
    return ListView(
      padding: const EdgeInsets.all(16),
      children: [
        Notice(
          icon: Icons.block,
          text: '실전 주문 꺼짐 · 로컬 거래 내역 가져오기와 가격·환율·지수 파일을 지원합니다.',
          color: warningColor(context),
        ),
        const SizedBox(height: 12),
        SectionCard(
          title: '연결 정보',
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('기본 주소: $baseUrl', style: tabular(context)),
              const SizedBox(height: 8),
              const Text(
                'API 주소는 앱 빌드 설정에서만 바꿀 수 있습니다. 캐시는 읽기 전용이며 오프라인 주문을 저장하지 않습니다.',
              ),
            ],
          ),
        ),
        const SizedBox(height: 12),
        if (ownerImports != null) ...[
          SectionCard(
            title: '가격·환율·비교 지수',
            child: OutlinedButton.icon(
              onPressed: () => Navigator.of(context).push(
                MaterialPageRoute<void>(
                  builder: (_) =>
                      OwnerDataImportPage(controller: ownerImports!),
                ),
              ),
              icon: const Icon(Icons.file_open_outlined),
              label: const Text('가격·환율·지수 파일 가져오기'),
            ),
          ),
          const SizedBox(height: 12),
        ],
        _buildReconciliation(context),
        const SizedBox(height: 12),
        _buildLocalOrders(context),
        const SizedBox(height: 12),
        SectionCard(
          title: '공시·뉴스·분석',
          child: OutlinedButton.icon(
            onPressed: () =>
                selectPublicResearch(context, dependencies.research),
            icon: const Icon(Icons.article_outlined),
            label: const Text('시장·종목 선택해 읽기'),
          ),
        ),
        const SizedBox(height: 12),
        SectionCard(
          title: '백테스트 보고서',
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const Text('가장 나중에 등록된 연구 결과 · 지표 재계산 안 함'),
              const SizedBox(height: 8),
              OutlinedButton.icon(
                onPressed: () => Navigator.of(context).push(
                  MaterialPageRoute<void>(
                    builder: (_) =>
                        ResearchReportPage(api: dependencies.research),
                  ),
                ),
                icon: const Icon(Icons.analytics_outlined),
                label: const Text('백테스트 보고서 보기'),
              ),
            ],
          ),
        ),
        const SizedBox(height: 12),
        SectionCard(
          title: '모의 전략 비교',
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const Text('저장된 모의 비교 기록을 요청할 때만 불러옵니다.'),
              const SizedBox(height: 8),
              OutlinedButton.icon(
                onPressed: () => Navigator.of(context).push(
                  MaterialPageRoute<void>(
                    builder: (_) =>
                        PaperPromotionReportPage(api: dependencies.paper),
                  ),
                ),
                icon: const Icon(Icons.compare_arrows),
                label: const Text('모의 전략 비교 보기'),
              ),
            ],
          ),
        ),
        const SizedBox(height: 12),
        SectionCard(
          title: '모의 자동매매 상태',
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const Text('저장된 로컬 모의 데이터를 요청할 때만 불러옵니다.'),
              const SizedBox(height: 4),
              Text(
                '프로세스 생존, 현재 증권사 상태 또는 실행 권한을 뜻하지 않습니다.',
                style: Theme.of(context).textTheme.bodySmall,
              ),
              const SizedBox(height: 12),
              OutlinedButton.icon(
                onPressed: () => Navigator.of(context).push(
                  MaterialPageRoute<void>(
                    builder: (_) => PaperMonitorPage(api: dependencies.paper),
                  ),
                ),
                icon: const Icon(Icons.monitor_heart_outlined),
                label: const Text('모의 자동매매 상태 보기'),
              ),
            ],
          ),
        ),
      ],
    );
  }

  Widget _buildLocalOrders(BuildContext context) {
    final orderLog = controller.orderLog;
    if (controller.orderLogBusy && orderLog == null) {
      return const SectionCard(
        title: '로컬 주문 기록',
        child: Loading(compact: true),
      );
    }
    if (controller.orderLogFailed && orderLog == null) {
      return SectionCard(
        title: '로컬 주문 기록',
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('주문 기록을 불러오지 못했습니다.'),
            const SizedBox(height: 12),
            ElevatedButton(
              onPressed: controller.refreshLocalOrders,
              child: const Text('로컬 주문 기록 다시 불러오기'),
            ),
          ],
        ),
      );
    }
    if (orderLog == null) return const SizedBox.shrink();
    return LocalOrderLogCard(
      orderLog: orderLog,
      retainedError: controller.orderLogFailed,
      busy: controller.orderLogBusy,
      onRefresh: controller.refreshLocalOrders,
    );
  }

  Widget _buildReconciliation(BuildContext context) {
    final reconciliation = controller.reconciliation;
    if (controller.reconciliationBusy && reconciliation == null) {
      return const SectionCard(
        title: '증권사 잔고 대조',
        child: Loading(compact: true),
      );
    }
    if (controller.reconciliationFailed && reconciliation == null) {
      return SectionCard(
        title: '증권사 잔고 대조',
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('대조 결과를 불러오지 못했습니다'),
            const SizedBox(height: 12),
            ElevatedButton(
              onPressed: controller.refreshReconciliation,
              child: const Text('대조 결과 다시 불러오기'),
            ),
          ],
        ),
      );
    }
    if (reconciliation == null) {
      return SectionCard(
        title: '증권사 잔고 대조',
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('아직 증권사 대조 기록이 없습니다'),
            const SizedBox(height: 4),
            Text(
              '로컬 원장 확인과 증권사 잔고 대조는 별개입니다.',
              style: Theme.of(context).textTheme.bodySmall,
            ),
          ],
        ),
      );
    }
    return BrokerReconciliationCard(
      reconciliation: reconciliation,
      retainedError: controller.reconciliationFailed,
      busy: controller.reconciliationBusy,
      onRefresh: controller.refreshReconciliation,
    );
  }
}
