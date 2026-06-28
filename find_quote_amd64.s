
#include "textflag.h"

// Сигнатура для функции: func findQuoteAsm(data []byte) (ret int)
// data (s_ptr, s_len): +0(FP), +8(FP)
// Невидимое пространство: +16(FP) (8 байт)
// ret: +24(FP)
TEXT ·findQuoteAsm(SB), NOSPLIT, $0-32 // 32 байта, чтобы покрыть все смещения

    // Читаем аргументы со стека.
    MOVQ s_ptr+0(FP), AX     // AX = указатель на начало среза.
    MOVQ s_len+8(FP), CX      // CX = длина среза.

    // Используем BX как счетчик индекса.
    XORQ BX, BX

LOOP:
    // Сравниваем индекс (BX) с длиной (CX).
    CMPQ BX, CX
    JGE  NOT_FOUND

    // Сравниваем байт по адресу [указатель + индекс].
    CMPB (AX)(BX*1), $'"'
    JE   FOUND

    // Продолжаем цикл.
    INCQ BX
    JMP  LOOP

FOUND:
    // Индекс в BX. Записываем его в ячейку для возвращаемого значения.
    MOVQ BX, ret+24(FP)
    RET

NOT_FOUND:
    // Символ не найден. Записываем -1.
    MOVQ $-1, ret+24(FP)
    RET


// func scanJSONStringASM(src []byte) (end int, hasEscape bool)
TEXT ·scanJSONStringASM(SB), NOSPLIT, $0-33
    MOVQ src_base+0(FP), AX
    MOVQ src_len+8(FP), BX

    XORQ CX, CX // index
    XORQ R8, R8  // hasEscape
    XORQ R9, R9  // escapeNext

scan_loop:
    CMPQ CX, BX
    JGE scan_eof

    MOVB (AX)(CX*1), AL

    TESTQ R9, R9
    JZ scan_not_escaped

    XORQ R9, R9
    INCQ CX
    JMP scan_loop

scan_not_escaped:
    CMPB AL, $0x5C
    JEQ scan_escape
    CMPB AL, $0x22
    JEQ scan_quote

    INCQ CX
    JMP scan_loop

scan_escape:
    MOVQ $1, R8
    MOVQ $1, R9
    INCQ CX
    JMP scan_loop

scan_quote:
    MOVQ CX, ret+24(FP)
    MOVB R8, hasEscape+32(FP)
    RET

scan_eof:
    MOVQ $-1, ret+24(FP)
    MOVB $0, hasEscape+32(FP)
    RET


// func appendStringASM(buf []byte, s string) []byte
TEXT ·appendStringASM(SB), NOSPLIT, $0-64
    MOVQ buf_base+0(FP), DI
    MOVQ buf_len+8(FP), R11
    MOVQ buf_cap+16(FP), DX
    MOVQ s_base+24(FP), R8
    MOVQ s_len+32(FP), R9

    // Проверяем, что в буфере хватает места для двух кавычек и содержимого строки.
    MOVQ R11, AX
    ADDQ R9, AX
    ADDQ $2, AX
    CMPQ AX, DX
    JG   append_string_overflow

    LEAQ (DI)(R11*1), DI
    MOVB $'"', 0(DI)
    INCQ DI
    MOVQ R8, SI
    MOVQ R9, CX

append_string_copy_loop:
    TESTQ CX, CX
    JZ   append_string_copy_done
    MOVB 0(SI), AL
    MOVB AL, 0(DI)
    INCQ SI
    INCQ DI
    DECQ CX
    JMP  append_string_copy_loop

append_string_copy_done:
    MOVB $'"', 0(DI)

    ADDQ R9, R11
    ADDQ $2, R11

    MOVQ buf_base+0(FP), AX
    MOVQ AX, ret_base+40(FP)
    MOVQ R11, ret_len+48(FP)
    MOVQ DX, ret_cap+56(FP)
    RET

append_string_overflow:
    MOVQ buf_base+0(FP), AX
    MOVQ AX, ret_base+40(FP)
    MOVQ buf_len+8(FP), AX
    MOVQ AX, ret_len+48(FP)
    MOVQ DX, ret_cap+56(FP)
    RET


// func findQuoteOrEscapeASM(b []byte) (idx int, isEscape bool)
TEXT ·findQuoteOrEscapeASM(SB), NOSPLIT, $0-33
    // Загружаем срез []byte (указатель, длина, вместимость)
    MOVQ b_base+0(FP), AX    // AX = указатель на данные
    MOVQ b_len+8(FP), BX     // BX = длина массива
    XORQ CX, CX              // CX = текущий индекс (начинаем с 0)

    // Бродкастим кавычку '"' (0x22) на все 32 байта регистра Y1
    MOVQ $0x2222222222222222, R8
    MOVQ R8, X1
    VPBROADCASTQ X1, Y1

    // Бродкастим слэш '\' (0x5c) на все 32 байта регистра Y2
    MOVQ $0x5c5c5c5c5c5c5c5c, R9
    MOVQ R9, X2
    VPBROADCASTQ X2, Y2

loop:
    // Проверяем, осталось ли хотя бы 32 байта для AVX2
    MOVQ BX, R10
    SUBQ CX, R10
    CMPQ R10, $32
    JL tail                  // Если меньше 32, уходим на хвост (Go обработает)

    // Загружаем сразу 32 байта из памяти в Y0
    VMOVDQU (AX)(CX*1), Y0

    // Векторное сравнение (SIMD)
    VPCMPEQB Y1, Y0, Y3      // Y3 = маска совпадений с кавычкой
    VPCMPEQB Y2, Y0, Y4      // Y4 = маска совпадений со слэшем

    // Объединяем маски (OR)
    VPOR Y3, Y4, Y5

    // Превращаем 256-битный результат в обычный 32-битный регистр
    VPMOVMSKB Y5, R11

    // Если R11 == 0, значит ни кавычек, ни слэшей в этих 32 байтах нет
    TESTL R11, R11
    JZ next_chunk

    // Нашли! Инструкция BSFL находит индекс первого установленного бита
    BSFL R11, R11

    // Проверяем, был ли это слэш (маска в Y4)
    VPMOVMSKB Y4, R12
    BTL R11, R12             // Проверяем бит под индексом R11
    JC is_escape

    // Это была кавычка
    ADDQ R11, CX             // Добавляем смещение к текущему индексу
    MOVQ CX, ret+24(FP)      // Возвращаем индекс
    MOVB $0, isEscape+32(FP) // Возвращаем false (не слэш)
    VZEROUPPER               // Сброс состояния AVX (обязательно для Go)
    RET

is_escape:
    ADDQ R11, CX
    MOVQ CX, ret+24(FP)
    MOVB $1, isEscape+32(FP) // Возвращаем true (это слэш)
    VZEROUPPER
    RET

next_chunk:
    ADDQ $32, CX             // Прыгаем сразу на 32 байта вперед
    JMP loop

tail:
    // Хвост добиваем побайтно: ищем либо кавычку, либо экранирующий слэш.
tail_loop:
    CMPQ CX, BX
    JGE tail_done

    MOVB (AX)(CX*1), AL
    CMPB AL, $0x22
    JEQ  tail_quote
    CMPB AL, $0x5C
    JEQ  tail_escape

    INCQ CX
    JMP  tail_loop

tail_quote:
    MOVQ CX, ret+24(FP)
    MOVB $0, isEscape+32(FP)
    VZEROUPPER
    RET

tail_escape:
    MOVQ CX, ret+24(FP)
    MOVB $1, isEscape+32(FP)
    VZEROUPPER
    RET

tail_done:
    MOVQ CX, ret+24(FP)
    MOVB $0, isEscape+32(FP)
    VZEROUPPER
    RET


// func parseShortStringASM2(src []byte) (written int, read int)
TEXT ·parseShortStringASM2(SB), NOSPLIT, $0-40
    MOVQ src_base+0(FP), SI
    MOVQ src_len+8(FP), BX

    XORQ CX, CX // readIdx
    XORQ DX, DX // writeIdx
loop:
    CMPQ CX, BX                // Проверка: прочитали всё?
    JGE eof

    MOVB (SI)(CX*1), AL        // Читаем байт src[readIdx]
    INCQ CX                    // readIdx++

    CMPB AL, $0x22             // Кавычка? (конец строки)
    JEQ done

    CMPB AL, $0x5C             // Слэш? (экранирование)
    JEQ escape

    MOVB AL, (SI)(DX*1)        // Обычный байт: пишем в src[writeIdx]
    INCQ DX                    // writeIdx++
    JMP loop

escape:
    MOVB (SI)(CX*1), AL        // Читаем байт после '\'
    INCQ CX

    // Простой маппинг спецсимволов
    CMPB AL, $0x6E       // 'n' -> 0x0A
    JEQ  is_n
    CMPB AL, $0x72       // 'r' -> 0x0D
    JEQ  is_r
    CMPB AL, $0x74       // 't' -> 0x09
    JEQ  is_t
    CMPB AL, $0x62       // 'b' -> 0x08
    JEQ  is_b
    CMPB AL, $0x66       // 'f' -> 0x0C
    JEQ  is_f
    // Если символ не в списке (например, \" или \\ или \/),
    // он просто запишется как есть (AL уже содержит нужный байт)
    JMP  write

is_n: MOVB $0x0A, AL; JMP write
is_r: MOVB $0x0D, AL; JMP write
is_t: MOVB $0x09, AL; JMP write
is_b: MOVB $0x08, AL; JMP write
is_f: MOVB $0x0C, AL; JMP write

check_r:
    CMPB AL, $0x72             // 'r' -> 0x0D
    JNE write
    MOVB $0x0D, AL

write:
    MOVB AL, (SI)(DX*1)        // Пишем разэкранированный байт в src[writeIdx]
    INCQ DX
    JMP loop

done:
    MOVQ DX, ret+24(FP)
    MOVQ CX, ret+32(FP)
    RET

eof:
    MOVQ $-1, ret+24(FP)
    MOVQ $-1, ret+32(FP)
    RET


// func parseShortStringASM(src []byte) ([]byte, int64)
TEXT ·parseShortStringASM(SB), $24-56
    MOVQ src_base+0(FP), R12
    MOVQ src_len+8(FP), BX

    XORQ CX, CX // readIdx
    XORQ R8, R8  // hadEscape
    MOVQ $0x2222222222222222, R9
    MOVQ $0x5c5c5c5c5c5c5c5c, R10
    MOVQ $0x0101010101010101, R11
    MOVQ $0x8080808080808080, R13

scan_loop:
    CMPQ CX, BX
    JGE scan_eof

    MOVQ BX, R14
    SUBQ CX, R14
    CMPQ R14, $8
    JL scan_tail

    MOVQ (R12)(CX*1), AX

    MOVQ AX, R14
    XORQ R9, R14
    MOVQ R14, R15
    SUBQ R11, R14
    NOTQ R15
    ANDQ R14, R15
    ANDQ R13, R15

    MOVQ AX, R14
    XORQ R10, R14
    MOVQ R14, SI
    SUBQ R11, R14
    NOTQ SI
    ANDQ R14, SI
    ANDQ R13, SI

    ORQ SI, R15
    TESTQ R15, R15
    JNZ scan_slow

    ADDQ $8, CX
    JMP scan_loop

scan_slow:
    MOVQ $8, R14

scan_slow_loop:
    CMPQ R14, $0
    JE scan_loop
    CMPQ CX, BX
    JGE scan_eof

    MOVB (R12)(CX*1), AL
    INCQ CX
    DECQ R14

    CMPB AL, $0x22
    JEQ scan_done
    CMPB AL, $0x5C
    JEQ scan_escape

    JMP scan_slow_loop

scan_escape:
    MOVQ $1, R8
    CMPQ CX, BX
    JGE scan_eof
    INCQ CX
    JMP scan_slow_loop

scan_tail:
scan_tail_loop:
    CMPQ CX, BX
    JGE scan_eof

    MOVB (R12)(CX*1), AL
    INCQ CX

    CMPB AL, $0x22
    JEQ scan_done
    CMPB AL, $0x5C
    JEQ scan_tail_escape

    JMP scan_tail_loop

scan_tail_escape:
    MOVQ $1, R8
    CMPQ CX, BX
    JGE scan_eof
    INCQ CX
    JMP scan_tail_loop

scan_done:
    MOVQ CX, R14
    CMPQ R8, $0
    JNE escaped_copy

    MOVQ R14, R15
    DECQ R15
    MOVQ R12, ret+24(FP)
    MOVQ R15, ret+32(FP)
    MOVQ R15, ret+40(FP)
    MOVQ R14, ret+48(FP)
    RET

escaped_copy:
    MOVQ R14, BX
    DECQ BX
    MOVQ BX, 0(SP)
    MOVQ R12, 8(SP)
    MOVQ R14, 16(SP)
    CALL ·allocBytes(SB)
    MOVQ 8(SP), R12
    MOVQ 16(SP), R14
    MOVQ AX, DI
    XORQ DX, DX
    XORQ R13, R13
    MOVQ R14, R15
    DECQ R15

copy_loop:
    CMPQ R13, R15
    JGE copy_done

    MOVB (R12)(R13*1), AL
    INCQ R13

    CMPB AL, $0x22
    JEQ copy_done
    CMPB AL, $0x5C
    JEQ copy_escape

    MOVB AL, (DI)(DX*1)
    INCQ DX
    JMP copy_loop

copy_escape:
    CMPQ R13, R15
    JGE copy_done

    MOVB (R12)(R13*1), AL
    INCQ R13

    CMPB AL, $0x6E
    JEQ copy_n
    CMPB AL, $0x72
    JEQ copy_r
    CMPB AL, $0x74
    JEQ copy_t
    CMPB AL, $0x62
    JEQ copy_b
    CMPB AL, $0x66
    JEQ copy_f
    CMPB AL, $0x22
    JEQ copy_quote
    CMPB AL, $0x5C
    JEQ copy_backslash
    CMPB AL, $0x2F
    JEQ copy_slash
    JMP copy_write

copy_n:
    MOVB $0x0A, AL
    JMP copy_write
copy_r:
    MOVB $0x0D, AL
    JMP copy_write
copy_t:
    MOVB $0x09, AL
    JMP copy_write
copy_b:
    MOVB $0x08, AL
    JMP copy_write
copy_f:
    MOVB $0x0C, AL
    JMP copy_write
copy_quote:
    MOVB $0x22, AL
    JMP copy_write
copy_backslash:
    MOVB $0x5C, AL
    JMP copy_write
copy_slash:
    MOVB $0x2F, AL
    JMP copy_write

copy_write:
    MOVB AL, (DI)(DX*1)
    INCQ DX
    JMP copy_loop

copy_done:
    MOVQ DI, ret+24(FP)
    MOVQ DX, ret+32(FP)
    MOVQ DX, ret+40(FP)
    MOVQ R14, ret+48(FP)
    RET

scan_eof:
    MOVQ $0, ret+24(FP)
    MOVQ $0, ret+32(FP)
    MOVQ $0, ret+40(FP)
    MOVQ $-1, ret+48(FP)
    RET

// func parseShortStringCopyASM(src []byte, dst []byte) int64
TEXT ·parseShortStringCopyASM(SB), NOSPLIT, $0-56
    MOVQ src_ptr+0(FP), SI
    MOVQ src_len+8(FP), BX
    MOVQ dst_ptr+24(FP), DI

    XORQ CX, CX // readIdx
    XORQ DX, DX // writeIdx

copy_loop:
    CMPQ CX, BX
    JGE copy_done

    MOVB (SI)(CX*1), AL
    INCQ CX

    CMPB AL, $0x22
    JEQ  copy_done
    CMPB AL, $0x5C
    JEQ  copy_escape

    MOVB AL, (DI)(DX*1)
    INCQ DX
    JMP copy_loop

copy_escape:
    CMPQ CX, BX
    JGE copy_eof

    MOVB (SI)(CX*1), AL
    INCQ CX

    CMPB AL, $0x6E
    JEQ  copy_n
    CMPB AL, $0x72
    JEQ  copy_r
    CMPB AL, $0x74
    JEQ  copy_t
    CMPB AL, $0x62
    JEQ  copy_b
    CMPB AL, $0x66
    JEQ  copy_f
    JMP  copy_write

copy_n:
    MOVB $0x0A, AL
    JMP copy_write
copy_r:
    MOVB $0x0D, AL
    JMP copy_write
copy_t:
    MOVB $0x09, AL
    JMP copy_write
copy_b:
    MOVB $0x08, AL
    JMP copy_write
copy_f:
    MOVB $0x0C, AL
    JMP copy_write

copy_write:
    MOVB AL, (DI)(DX*1)
    INCQ DX
    JMP copy_loop

copy_done:
    MOVQ DX, written+48(FP)
    RET

copy_eof:
    MOVQ $-1, written+48(FP)
    RET

// func findObjectBoundariesASM(data []byte, chunks []Chunk) (ret0 int, ret1 int)
TEXT ·findObjectBoundariesASM(SB), NOSPLIT, $0-64
    // 1. Аргументы:
    // data_ptr (0), data_len (8), data_cap (16)
    // chunks_ptr (24), chunks_len (32), chunks_cap (40)
    MOVQ data_base+0(FP), SI
    MOVQ data_len+8(FP), BX
    MOVQ chunks_base+24(FP), DI
    MOVQ chunks_len+32(FP), DX

    // 2. SIMD-константы
    MOVQ $0x2222222222222222, R14
    MOVQ R14, X1
    VPBROADCASTQ X1, Y1

    MOVQ $0x5c5c5c5c5c5c5c5c, R14
    MOVQ R14, X2
    VPBROADCASTQ X2, Y2

    MOVQ $0x7b7b7b7b7b7b7b7b, R14
    MOVQ R14, X3
    VPBROADCASTQ X3, Y3

    MOVQ $0x7d7d7d7d7d7d7d7d, R14
    MOVQ R14, X4
    VPBROADCASTQ X4, Y4

    MOVQ $0x5b5b5b5b5b5b5b5b, R14
    MOVQ R14, X5
    VPBROADCASTQ X5, Y5

    MOVQ $0x5d5d5d5d5d5d5d5d, R14
    MOVQ R14, X6
    VPBROADCASTQ X6, Y6

    // 3. Инициализация
    XORQ CX, CX        // i
    XORQ R8, R8        // totalCount
    XORQ R9, R9        // objectDepth
    XORQ R12, R12      // totalDepth
    MOVQ $-1, R10      // start
    XORQ R11, R11      // inString
    XORQ R13, R13      // storedCount

    // Защита от nil
    TESTQ SI, SI
    JZ    done
    TESTQ DI, DI
    JZ    done

loop:
    CMPQ CX, BX
    JGE  done
    TESTQ R11, R11
    JNZ   string_scan

outside_scan:
    MOVB (SI)(CX*1), AL
    CMPB AL, $34
    JL   not_special
    CMPB AL, $125
    JG   not_special

    CMPB AL, $0x22
    JEQ  outside_quote
    CMPB AL, $0x5C
    JEQ  outside_backslash
    CMPB AL, $0x7B
    JEQ  outside_open_brace
    CMPB AL, $0x7D
    JEQ  outside_close_brace
    CMPB AL, $0x5B
    JEQ  outside_open_bracket
    CMPB AL, $0x5D
    JEQ  outside_close_bracket

not_special:
    INCQ CX
    JMP  loop

outside_quote:
    XORQ $1, R11
    INCQ CX
    JMP  loop

outside_backslash:
    INCQ CX
    JMP  loop

outside_open_brace:
    INCQ R9
    INCQ R12
    CMPQ R9, $1
    JNE  outside_open_brace_step
    MOVQ CX, R10

outside_open_brace_step:
    INCQ CX
    JMP  loop

outside_close_brace:
    TESTQ R9, R9
    JZ   outside_close_bracket
    DECQ R9
    TESTQ R12, R12
    JZ   outside_close_brace_advance
    DECQ R12
    TESTQ R9, R9
    JNZ  outside_close_brace_advance

    INCQ R8
    CMPQ R13, DX
    JGE  outside_close_brace_reset

    MOVQ R13, AX
    SHLQ $4, AX
    MOVQ R10, 0(DI)(AX*1)
    MOVQ CX, 8(DI)(AX*1)
    INCQ 8(DI)(AX*1)
    INCQ R13

outside_close_brace_reset:
    MOVQ $-1, R10
outside_close_brace_advance:
    INCQ CX
    JMP  loop

outside_close_bracket:
    TESTQ R12, R12
    JZ   outside_close_brace_advance
    DECQ R12
    INCQ CX
    JMP  loop

outside_open_bracket:
    INCQ R12
    INCQ CX
    JMP  loop

string_scan:
    MOVQ BX, R15
    SUBQ CX, R15
    CMPQ R15, $32
    JL   string_tail

    VMOVDQU (SI)(CX*1), Y0
    VPCMPEQB Y1, Y0, Y7
    VPCMPEQB Y2, Y0, Y8
    VPOR Y7, Y8, Y9
    VPMOVMSKB Y9, R15
    TESTL R15, R15
    JZ   string_advance

    BSFL R15, R14
    VPMOVMSKB Y8, R15
    BTL R14, R15
    JC  string_escape

    ADDQ R14, CX
    XORQ $1, R11
    INCQ CX
    JMP  loop

string_escape:
    ADDQ R14, CX
    ADDQ $2, CX
    JMP  loop

string_advance:
    ADDQ $32, CX
    JMP  loop

string_tail:
    CMPQ CX, BX
    JGE  done
    MOVB (SI)(CX*1), AL
    CMPB AL, $0x5C
    JEQ  string_tail_escape
    CMPB AL, $0x22
    JEQ  string_tail_quote
    INCQ CX
    JMP  string_tail

string_tail_escape:
    ADDQ $2, CX
    JMP  loop

string_tail_quote:
    XORQ $1, R11
    INCQ CX
    JMP  loop

done:
    MOVQ R8, ret0+48(FP)
    MOVQ R12, ret1+56(FP)
    VZEROUPPER
    RET

// func skipValueASM(raw []byte, start int) int
TEXT ·skipValueASM(SB), NOSPLIT, $0-24
    // Загрузка аргументов (ABI Go)
    MOVQ raw_base+0(FP), SI
    MOVQ raw_len+8(FP), BX
    MOVQ start+24(FP), CX

    // Проверка границ на входе
    CMPQ CX, BX
    JGE  done_err

loop:
    CMPQ CX, BX
    JGE  done_err

    MOVB (SI)(CX*1), AL

    // Поиск кавычки
    CMPB AL, $0x22
    JEQ  skip_string

    // Остановка на структуре
    CMPB AL, $0x2C
    JEQ  done
    CMPB AL, $0x7D
    JEQ  done
    CMPB AL, $0x5D
    JEQ  done

    INCQ CX
    JMP  loop

skip_string:
    INCQ CX
string_loop:
    CMPQ CX, BX
    JGE  done_err

    MOVB (SI)(CX*1), AL

    CMPB AL, $0x5C // '\'
    JEQ  handle_escape

    CMPB AL, $0x22 // '"'
    JEQ  next_loop

    INCQ CX
    JMP  string_loop

handle_escape:
    ADDQ $2, CX
    JMP  string_loop

next_loop:
    INCQ CX
    JMP  loop

done:
    MOVQ CX, ret+32(FP)
    RET

done_err:
    MOVQ BX, ret+32(FP)
    RET

// func appendIntASM(buf []byte, val int64) []byte
// Возвращает новый срез (base, len, cap)
TEXT ·appendIntASM(SB), NOSPLIT, $0-56
    MOVQ buf_base+0(FP), DI    // Base
    MOVQ buf_len+8(FP), SI     // Len
    MOVQ buf_cap+16(FP), DX    // Cap
    MOVQ val+24(FP), AX        // Val

    // DEBUG: Если Base == 0, значит срез не инициализирован
    TESTQ DI, DI
    JZ   overflow

    // ЗАЩИТА: Если len + 20 > cap, возвращаем старый буфер,
    // чтобы Go сделал append (расширение)
    MOVQ SI, R8
    ADDQ $20, R8
    CMPQ R8, DX
    JGT  overflow

    // Проверка на 0
    TESTQ AX, AX
    JNE   check_negative
    MOVB  $0x30, 0(DI)(SI*1)
    INCQ  SI
    JMP   done

check_negative:
    JGE   prepare_div
    MOVB  $0x2D, 0(DI)(SI*1)
    INCQ  SI
    NEGQ  AX

prepare_div:
    MOVQ  SI, R8               // Начало числа в буфере
    MOVQ  $10, CX

loop:

    CMPQ SI, DX    // SI = len, DX = cap
    JGE  overflow  // Если длина >= емкости, мы не можем писать!

    MOVQ  AX, BX
    MOVQ  $0xCCCCCCCCCCCCCCCD, R9
    IMULQ R9
    MOVQ  DX, AX
    SHRQ  $3, AX
    // Коррекция частного из-за сдвига
    MOVQ  AX, R10
    IMULQ $10, R10
    SUBQ  R10, BX

    ADDQ  $0x30, BX
    MOVB  BL, 0(DI)(SI*1)
    INCQ  SI

    TESTQ AX, AX
    JNE   loop

    // Переворачиваем (Reverse)
    MOVQ  R8, DX
    MOVQ  SI, CX
    DECQ  CX

reverse:
    CMPQ  DX, CX
    JGE   done
    MOVB  (DI)(DX*1), R10
    MOVB  (DI)(CX*1), R11
    MOVB  R11, (DI)(DX*1)
    MOVB  R10, (DI)(CX*1)
    INCQ  DX
    DECQ  CX
    JMP   reverse

overflow:
    // Возвращаем исходный срез (как он пришел на вход)
    MOVQ buf_base+0(FP), AX  // Восстанавливаем Base
    MOVQ AX, ret_base+32(FP) // Записываем в ret (Base)
    MOVQ SI, ret_len+40(FP)  // Записываем исходную длину (Len)
    MOVQ DX, ret_cap+48(FP)  // Записываем исходную емкость (Cap)
    RET

done:
    // Возвращаем обновленный срез
    MOVQ buf_base+0(FP), AX  // Base тот же
    MOVQ AX, ret_base+32(FP)
    MOVQ SI, ret_len+40(FP)  // Новая длина (SI обновился в процессе)
    MOVQ DX, ret_cap+48(FP)  // Cap тот же
    RET

