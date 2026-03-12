pipeline {

    agent any

    environment {
        PROJECT_NAME = "RSS Feed 서버"
        BUILD_TIMESTAMP = sh(script: "date -u +'%Y-%m-%dT%H:%M:%SZ'", returnStdout: true).trim()
        DOCKER_IMAGE_NAME = "darkkaiser/rss-feed-server"
        COVERAGE_THRESHOLD = "50"
    }

    stages {

        stage('환경 검증') {
            steps {
                script {
                    // 필수 환경 변수 확인
                    echo "환경 변수 검증 중..."
                    
                    if (!env.TELEGRAM_BOT_TOKEN) {
                        error("필수 환경 변수가 설정되지 않았습니다: TELEGRAM_BOT_TOKEN")
                    }
                    
                    if (!env.TELEGRAM_CHAT_ID) {
                        error("필수 환경 변수가 설정되지 않았습니다: TELEGRAM_CHAT_ID")
                    }
                    
                    echo "환경 검증 완료"
                    echo "빌드 타임스탬프: ${env.BUILD_TIMESTAMP}"
                }
            }
        }

        stage('준비') {
            steps {
                cleanWs()
            }
        }

        stage('소스 체크아웃') {
            steps {
                checkout([
                    $class: 'GitSCM',
                    branches: [[ name: '*/main' ]],
                    extensions: [[
                        $class: 'SubmoduleOption',
                        disableSubmodules: false,
                        parentCredentials: true,
                        recursiveSubmodules: false,
                        reference: '',
                        trackingSubmodules: true
                    ]],
                    submoduleCfg: [],
                    userRemoteConfigs: [[
                        credentialsId: 'github-darkkaiser-credentials',
                        url: 'https://github.com/DarkKaiser/rss-feed-server.git'
                    ]]
                ])

                // Git 정보 설정
                script {
                    // 태그 정보가 없으면 커밋 해시만 반환 (--always)
                    env.APP_VERSION = sh(script: "git describe --tags --always --dirty", returnStdout: true).trim()

                    env.GIT_COMMIT_HASH_SHORT = sh(script: "git rev-parse --short HEAD", returnStdout: true).trim()
                    env.GIT_COMMIT_HASH_FULL = sh(script: "git rev-parse HEAD", returnStdout: true).trim()

                    // Git 트리 상태 (clean / dirty) 확인
                    env.GIT_TREE_STATE = sh(script: 'if [ -z "$(git status --porcelain)" ]; then echo "clean"; else echo "dirty"; fi', returnStdout: true).trim()
                    
                    echo "Git 정보:"
                    echo "  버전: ${env.APP_VERSION}"
                    echo "  상태: ${env.GIT_TREE_STATE}"
                    echo "  커밋: ${env.GIT_COMMIT_HASH_SHORT} (${env.GIT_COMMIT_HASH_FULL})"
                    echo "  빌드: #${env.BUILD_NUMBER}"
                }
            }
        }

        stage('테스트 및 코드 품질 검사') {
            steps {
                script {
                    // Dockerfile의 builder 스테이지에서 테스트와 린트(golangci-lint)가 모두 실행됩니다.
                    // 이를 통해 Jenkins 환경(DinD)에서의 볼륨 마운트 문제를 해결하고, 일관된 검사 환경을 보장합니다.
                    
                    echo "빌드 및 테스트 시작..."
                    sh """
                        docker build --target builder \\
                            --build-arg APP_VERSION=${env.APP_VERSION} \\
                            --build-arg GIT_COMMIT_HASH=${env.GIT_COMMIT_HASH_SHORT} \\
                            --build-arg GIT_TREE_STATE=${env.GIT_TREE_STATE} \\
                            --build-arg BUILD_DATE=${env.BUILD_TIMESTAMP} \\
                            --build-arg BUILD_NUMBER=${env.BUILD_NUMBER} \\
                            -t rss-feed-server:test .
                    """
                    
                    echo "테스트 커버리지 추출 중..."
                    sh '''
                        # 컨테이너에서 커버리지 파일 추출
                        docker create --name temp-coverage rss-feed-server:test
                        docker cp temp-coverage:/go/src/app/coverage.out ./coverage.out || echo "커버리지 파일을 찾을 수 없습니다."
                        docker rm temp-coverage
                    '''
                    
                    // 커버리지 리포트 생성 (선택적)
                    if (fileExists('coverage.out')) {
                        echo "테스트 커버리지 분석 중..."
                        sh '''
                            # 커버리지 요약 출력
                            docker run --rm -v $(pwd)/coverage.out:/coverage.out rss-feed-server:test \\
                                go tool cover -func=/coverage.out | tail -n 1
                        '''
                        
                        // 커버리지 파일 아카이빙
                        archiveArtifacts artifacts: 'coverage.out', allowEmptyArchive: true
                    }
                }
            }
        }

        /*
        stage('보안 스캔') {
            steps {
                // Trivy를 사용하여 빌드된 이미지(rss-feed-server:test)의 취약점을 스캔합니다.
                // 파일 시스템 스캔(fs) 대신 이미지 스캔(image)을 사용하여 볼륨 마운트 문제를 회피합니다.
                // --exit-code 0: 취약점이 발견되어도 빌드를 실패시키지 않고 경고만 남깁니다.
                // --severity HIGH,CRITICAL: 심각도가 높거나 치명적인 취약점만 검사합니다.
                sh 'docker run --rm -v /var/run/docker.sock:/var/run/docker.sock aquasec/trivy:latest image --exit-code 0 --severity HIGH,CRITICAL rss-feed-server:test'
            }
        }
        */
        
        // 보안 스캔 단계는 실행 시간이 오래 걸려 주석 처리했습니다.
        // 필요 시 수동으로 실행하거나, 야간 빌드 등 별도 스케줄로 분리하는 것을 권장합니다.
        
        stage('테스트 이미지 정리') {
            steps {
                // 테스트용 이미지는 더 이상 필요하지 않으므로 삭제
                // || true를 사용하여 이미지가 없어도 에러가 발생하지 않도록 함
                sh 'docker rmi rss-feed-server:test || true'
            }
        }
        
        stage('도커 이미지 빌드') {
            steps {
                script {
                    echo "프로덕션 이미지 빌드 중..."
                    
                    // 버전 태그 생성 (Git Version 사용)
                    env.VERSION_TAG = "${env.APP_VERSION}"
                    
                    sh """
                        docker build \\
                            --build-arg APP_VERSION=${env.APP_VERSION} \\
                            --build-arg GIT_COMMIT_HASH=${env.GIT_COMMIT_HASH_SHORT} \\
                            --build-arg GIT_TREE_STATE=${env.GIT_TREE_STATE} \\
                            --build-arg BUILD_DATE=${env.BUILD_TIMESTAMP} \\
                            --build-arg BUILD_NUMBER=${env.BUILD_NUMBER} \\
                            -t ${env.DOCKER_IMAGE_NAME}:latest \\
                            -t ${env.DOCKER_IMAGE_NAME}:${env.VERSION_TAG} \\
                            .
                    """
                    
                    echo "이미지 빌드 완료"
                    echo "생성된 태그: latest, ${env.VERSION_TAG}"
                }
            }
        }

        stage('도커 컨테이너 실행') {
            steps {
                script {
                    // 기존 컨테이너 중지 및 제거 (안전한 방식)
                    sh '''
                        if docker ps -a --filter name=rss-feed-server --format '{{.Names}}' | grep -q '^rss-feed-server$'; then
                            echo "기존 컨테이너 중지 중..."
                            docker container stop rss-feed-server || true
                            echo "기존 컨테이너 제거 중..."
                            docker container rm rss-feed-server || true
                        else
                            echo "기존 컨테이너가 없습니다."
                        fi
                    '''
                    
                    // 새 컨테이너 실행
                    echo "새 컨테이너 시작 중..."
                    echo "사용할 이미지: ${env.DOCKER_IMAGE_NAME}:latest (빌드 버전: ${env.VERSION_TAG})"
                    sh """
                        docker run -d --name rss-feed-server \\
                                      -e TZ=Asia/Seoul \\
                                      -v /usr/local/docker/rss-feed-server:/usr/local/app \\
                                      -v /usr/local/docker/nginx-proxy-manager/letsencrypt:/etc/letsencrypt:ro \\
                                      -p 3443:3443 \\
                                      --add-host=api.darkkaiser.com:192.168.219.110 \\
                                      --restart=\"always\" \\
                                      ${env.DOCKER_IMAGE_NAME}:latest
                    """
                    
                    // 컨테이너 상태 확인
                    sh '''
                        echo "컨테이너 상태 확인 중..."
                        sleep 3
                        docker ps --filter name=rss-feed-server --format 'table {{.Names}}\\t{{.Status}}\\t{{.Ports}}'
                    '''
                }
            }
        }

        stage('도커 이미지 정리') {
            steps {
                script {
                    echo "이전 버전 이미지 정리 중..."
                    
                    // dangling 이미지 정리
                    sh 'docker images -qf dangling=true | xargs -r docker rmi || echo "정리할 dangling 이미지가 없습니다."'
                    
                    // 오래된 버전 이미지 정리 (최근 5개 버전만 유지)
                    sh """
                        docker images ${env.DOCKER_IMAGE_NAME} --format '{{.Tag}}' | \\
                        grep -v '^latest\$' | \\
                        tail -n +6 | \\
                        xargs -r -I {} docker rmi ${env.DOCKER_IMAGE_NAME}:{} || echo "정리할 오래된 이미지가 없습니다."
                    """
                }
            }
        }

    }

    post {

        success {
            script {
                def message = """【 알림 > Jenkins > ${env.PROJECT_NAME} 】

✅ 빌드 작업이 성공하였습니다.

커밋: ${env.GIT_COMMIT_HASH_SHORT}
빌드: #${env.BUILD_NUMBER}
버전: ${env.VERSION_TAG}
시간: ${env.BUILD_TIMESTAMP}

${env.BUILD_URL}"""
                
                sh """
                    curl -s -X POST "https://api.telegram.org/bot${env.TELEGRAM_BOT_TOKEN}/sendMessage" \\
                        -d "chat_id=${env.TELEGRAM_CHAT_ID}" \\
                        --data-urlencode "text=${message}"
                """
            }
        }

        failure {
            script {
                def message = """【 알림 > Jenkins > ${env.PROJECT_NAME} 】

❌ 빌드 작업이 실패하였습니다.

커밋: ${env.GIT_COMMIT_HASH_SHORT}
빌드: #${env.BUILD_NUMBER}
시간: ${env.BUILD_TIMESTAMP}

${env.BUILD_URL}"""
                
                sh """
                    curl -s -X POST "https://api.telegram.org/bot${env.TELEGRAM_BOT_TOKEN}/sendMessage" \\
                        -d "chat_id=${env.TELEGRAM_CHAT_ID}" \\
                        --data-urlencode "text=${message}"
                """
            }
        }

        always {
            cleanWs()
        }

    }

}
