#!/bin/bash


# 文件名，使用 $i 作为后缀
    output_file="test_results3d.txt"
# 清空输出文件
    echo "" > "$output_file"

# 运行100次测试
for ((i=1; i<=150; i++)); do
    echo "Running test $i..."

    echo "Running test $i...." >> "$output_file"
    # 执行测试并将结果输出到对应的文件
    go test -run 3D >> "$output_file"
    echo "end of test $i..." >> "$output_file"
    echo "end of test $i..."
done

echo "All tests completed. Results are saved in $output_file"