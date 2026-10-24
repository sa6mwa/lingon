package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"pkt.systems/lingon"
)

const geometryFlagName = "geometry"

func parseGeometry(raw string) (int, int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, 0, fmt.Errorf("geometry must use COLSxROWS format, for example 80x24")
	}
	value = strings.ToLower(value)
	if strings.Count(value, "x") != 1 {
		return 0, 0, fmt.Errorf("invalid geometry %q: expected COLSxROWS, for example 80x24", raw)
	}
	rawCols, rawRows, _ := strings.Cut(value, "x")
	cols, err := parseGeometryDimension(rawCols, "columns", raw)
	if err != nil {
		return 0, 0, err
	}
	rows, err := parseGeometryDimension(rawRows, "rows", raw)
	if err != nil {
		return 0, 0, err
	}
	return cols, rows, nil
}

func parseGeometryDimension(raw, name, original string) (int, error) {
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid geometry %q: %s must be a non-negative integer", original, name)
	}
	if value < 0 {
		return 0, fmt.Errorf("invalid geometry %q: %s must be zero or greater", original, name)
	}
	return value, nil
}

func defaultGeometryZeros(cols, rows int) (int, int) {
	if cols == 0 {
		cols = lingon.DefaultTerminalCols
	}
	if rows == 0 {
		rows = lingon.DefaultTerminalRows
	}
	return cols, rows
}

func resolveRootHostSize(cmd *cobra.Command, colsFlag, rowsFlag int) (int, int, error) {
	if cmd != nil && cmd.Flags().Changed(geometryFlagName) {
		raw, err := cmd.Flags().GetString(geometryFlagName)
		if err != nil {
			return 0, 0, err
		}
		return parseGeometry(raw)
	}
	colsValue := colsFlag
	if cmd == nil || !cmd.Flags().Changed("cols") {
		colsValue = 0
	}
	rowsValue := rowsFlag
	if cmd == nil || !cmd.Flags().Changed("rows") {
		rowsValue = 0
	}
	return colsValue, rowsValue, nil
}

func resolveHeadlessSize(cmd *cobra.Command) (int, int, error) {
	if cmd == nil {
		return lingon.DefaultTerminalCols, lingon.DefaultTerminalRows, nil
	}
	if cmd.Flags().Changed(geometryFlagName) {
		raw, err := cmd.Flags().GetString(geometryFlagName)
		if err != nil {
			return 0, 0, err
		}
		colsValue, rowsValue, err := parseGeometry(raw)
		if err != nil {
			return 0, 0, err
		}
		colsValue, rowsValue = defaultGeometryZeros(colsValue, rowsValue)
		return colsValue, rowsValue, nil
	}
	colsValue := lingon.DefaultTerminalCols
	rowsValue := lingon.DefaultTerminalRows
	if cmd.Flags().Changed("cols") {
		value, err := cmd.Flags().GetInt("cols")
		if err != nil {
			return 0, 0, err
		}
		colsValue = value
	}
	if cmd.Flags().Changed("rows") {
		value, err := cmd.Flags().GetInt("rows")
		if err != nil {
			return 0, 0, err
		}
		rowsValue = value
	}
	return colsValue, rowsValue, nil
}
