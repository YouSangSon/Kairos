import 'package:flutter/material.dart';

ThemeData theme(Brightness brightness) {
  final dark = brightness == Brightness.dark;
  final canvas = Color(dark ? 0xFF17171C : 0xFFF2F4F6);
  final raised = Color(dark ? 0xFF202027 : 0xFFFFFFFF);
  final primary = Color(dark ? 0xFFF2F4F6 : 0xFF191F28);
  final secondary = Color(dark ? 0xFFB0B8C1 : 0xFF6B7684);
  final action = Color(dark ? 0xFF60A5FA : 0xFF2563EB);
  return ThemeData(
    useMaterial3: true,
    fontFamily: 'NotoSansKR',
    brightness: brightness,
    scaffoldBackgroundColor: canvas,
    colorScheme: ColorScheme(
      brightness: brightness,
      primary: action,
      onPrimary: dark ? const Color(0xFF0F172A) : Colors.white,
      secondary: action,
      onSecondary: dark ? const Color(0xFF0F172A) : Colors.white,
      error: dark ? const Color(0xFFFCA5A5) : const Color(0xFFB91C1C),
      onError: dark ? const Color(0xFF0F172A) : Colors.white,
      surface: raised,
      onSurface: primary,
    ),
    appBarTheme: AppBarTheme(
      backgroundColor: canvas,
      foregroundColor: primary,
      surfaceTintColor: Colors.transparent,
      centerTitle: false,
      elevation: 0,
      titleSpacing: 20,
      titleTextStyle: TextStyle(
        fontFamily: 'NotoSansKR',
        color: primary,
        fontSize: 22,
        fontWeight: FontWeight.w700,
      ),
    ),
    navigationBarTheme: NavigationBarThemeData(
      backgroundColor: raised,
      elevation: 0,
      indicatorColor: action.withValues(alpha: 0.12),
      iconTheme: WidgetStateProperty.resolveWith(
        (states) => IconThemeData(
          color: states.contains(WidgetState.selected) ? action : secondary,
        ),
      ),
      labelTextStyle: WidgetStateProperty.resolveWith(
        (states) => TextStyle(
          fontFamily: 'NotoSansKR',
          color: states.contains(WidgetState.selected) ? action : secondary,
          fontSize: 12,
          fontWeight: states.contains(WidgetState.selected)
              ? FontWeight.w700
              : FontWeight.w500,
        ),
      ),
    ),
    textTheme: TextTheme(
      bodyLarge: TextStyle(fontSize: 16, color: primary),
      bodyMedium: TextStyle(fontSize: 16, color: primary),
      bodySmall: TextStyle(fontSize: 13, color: secondary),
      titleMedium: TextStyle(
        fontSize: 20,
        fontWeight: FontWeight.w700,
        color: primary,
      ),
      headlineMedium: TextStyle(
        fontSize: 32,
        fontWeight: FontWeight.w700,
        color: primary,
        fontFeatures: const [FontFeature.tabularFigures()],
      ),
    ),
    cardTheme: CardThemeData(
      color: raised,
      elevation: 0,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(16),
        side: BorderSide.none,
      ),
    ),
    dividerTheme: DividerThemeData(
      color: dark ? const Color(0xFF3A3A43) : const Color(0xFFD1D6DB),
      thickness: 1,
      space: 1,
    ),
    elevatedButtonTheme: ElevatedButtonThemeData(
      style: ElevatedButton.styleFrom(
        minimumSize: const Size(double.infinity, 52),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(14)),
        textStyle: const TextStyle(
          fontFamily: 'NotoSansKR',
          fontSize: 16,
          fontWeight: FontWeight.w700,
        ),
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: OutlinedButton.styleFrom(
        minimumSize: const Size(double.infinity, 52),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(14)),
        textStyle: const TextStyle(
          fontFamily: 'NotoSansKR',
          fontSize: 16,
          fontWeight: FontWeight.w700,
        ),
      ),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: raised,
      border: OutlineInputBorder(
        borderRadius: BorderRadius.circular(14),
        borderSide: BorderSide.none,
      ),
      contentPadding: EdgeInsets.all(16),
    ),
  );
}
